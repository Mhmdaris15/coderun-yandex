package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func submitCmd() *cobra.Command {
	var file, lang string

	cmd := &cobra.Command{
		Use:   "submit <selection> <slug>",
		Short: "Submit a source file and wait for the verdict",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, cfg *config.Config, b *pwclient.Browser, st *storage.Store) error {
				ref := coderun.ProblemRef{SelectionSlug: args[0], ProblemSlug: args[1]}

				fmt.Println("[1/4] Fetching problem...")
				p, contextID, err := b.GetProblem(ctx, ref)
				if err != nil {
					return err
				}
				ref.ContextID = contextID
				if err := st.SaveProblem(ctx, p); err != nil {
					return err
				}
				if contextID != 0 {
					if err := st.SetContextID(ctx, ref); err != nil {
						return err
					}
				}

				slug, err := resolveCompiler(lang, p.Languages)
				if err != nil {
					return err
				}

				ext := strings.TrimPrefix(filepath.Ext(file), ".")

				// Submit reads and validates the source file itself, before
				// touching the browser, and returns exactly those bytes
				// alongside the submission. Using its return value (rather
				// than reading the file again here) guarantees the artifact
				// recorded below is byte-for-byte what was uploaded, even if
				// the file on disk changes in between.
				fmt.Printf("[2/4] Submitting %s as %s...\n", file, slug)
				sub, source, err := b.Submit(ctx, ref, slug, file)
				unconfirmed := errors.Is(err, pwclient.ErrSubmitUnconfirmed)
				if err != nil && !unconfirmed {
					// Nothing was confirmed submitted and there is nothing to
					// recover: no attempt to reserve, nothing to record.
					return err
				}
				if unconfirmed {
					fmt.Println("      submission not confirmed by the server")
				} else {
					fmt.Printf("      submission %s\n", sub.GlobalID)
				}

				// A submission may now exist on CodeRun's servers — confirmed,
				// or not. Every exit path from here must leave the user able to
				// find it again.
				attempt, attemptErr := st.NextAttempt(ctx, ref.SelectionSlug, ref.ProblemSlug, slug)
				if attemptErr != nil {
					// No attempt number means no artifact filename. The globalId
					// (if any) goes into the error text, because it is the only
					// handle that can recover this submission.
					return fmt.Errorf("submission %s was sent to CodeRun but no attempt number could be reserved: %w",
						sub.GlobalID, attemptErr)
				}

				// record persists everything known about sub so far — the
				// write-only artifact sidecar for humans, and the submission
				// (plus any judged tests) in SQLite, which is what the program
				// itself ever reads back.
				record := func(verdict string, tests []coderun.TestResult) error {
					if werr := storage.WriteAttempt("solutions", ref, attempt, ext, source, storage.AttemptMeta{
						Language:     slug,
						SubmissionID: sub.GlobalID,
						Verdict:      verdict,
					}); werr != nil {
						return werr
					}
					if serr := st.SaveSubmission(ctx, sub, attempt); serr != nil {
						return serr
					}
					if len(tests) > 0 {
						if terr := st.SaveTestResults(ctx, sub.GlobalID, tests); terr != nil {
							return terr
						}
					}
					return nil
				}

				if unconfirmed {
					if rerr := record("UNCONFIRMED", nil); rerr != nil {
						return fmt.Errorf("submission may exist but is unconfirmed, and recording the attempt also failed: %v (original: %w)", rerr, err)
					}
					return fmt.Errorf("submission was not confirmed by the server; a submission may exist on CodeRun — check the site: %w", err)
				}

				fmt.Println("[3/4] Waiting for the verdict...")
				final, verdictErr := b.AwaitVerdict(ctx, sub.GlobalID, cfg.PollInterval, cfg.SubmissionTimeout)
				if verdictErr != nil {
					// Record what we know before surfacing the failure. The
					// globalId is the only handle that can recover this
					// submission later.
					if err := record("UNKNOWN", nil); err != nil {
						return fmt.Errorf("could not read the verdict (%v), and recording the attempt also failed: %w", verdictErr, err)
					}
					return fmt.Errorf("submission %s saved as attempt %d, but the verdict could not be read: %w",
						sub.GlobalID, attempt, verdictErr)
				}
				final.Ref = ref
				sub = final // record() below persists the fully judged submission

				fmt.Println("[4/4] Recording the attempt...")
				if err := record(final.Verdict, final.OpenTests); err != nil {
					return err
				}

				printVerdict(final)
				if !coderun.IsAccepted(final.Verdict) {
					return fmt.Errorf("not accepted: %s", final.Verdict)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "path to the source file (required)")
	cmd.Flags().StringVar(&lang, "lang", "python", "language name or compiler slug")
	cmd.MarkFlagRequired("file")
	return cmd
}

func printVerdict(s *coderun.Submission) {
	fmt.Printf("\nVerdict: %s\n", s.Verdict)
	fmt.Printf("Time:    %d ms (limit %d ms)\n", s.MaxTimeMillis, s.TimeLimitMillis)
	fmt.Printf("Memory:  %d bytes (limit %d bytes)\n", s.MaxMemoryBytes, s.MemoryLimitBytes)

	if s.CompileLog != "" {
		fmt.Printf("\nCompile log:\n%s\n", s.CompileLog)
	}
	if coderun.IsAccepted(s.Verdict) {
		return
	}
	if s.FirstFailedTest > 0 {
		fmt.Printf("\nFirst failed test: %d (of %d sample + %d hidden)\n",
			s.FirstFailedTest, len(s.OpenTests), s.HiddenTestCount)
	}
	for _, t := range s.OpenTests {
		if coderun.IsAccepted(t.Verdict) || t.Input == "" {
			continue
		}
		fmt.Printf("\nTest %d — %s\nInput:\n%s\nExpected:\n%s\nActual:\n%s\n",
			t.Number, t.Verdict, t.Input, t.Answer, t.Output)
	}
}
