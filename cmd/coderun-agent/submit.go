package main

import (
	"context"
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

				// Read the source BEFORE submitting. Submitting is the point of
				// no return: it takes an action on a live platform that counts
				// against the user's record. Anything that can fail must fail
				// before it, so that nothing sits between the submission and the
				// record of that submission.
				source, err := readFile(file)
				if err != nil {
					return err
				}
				ext := strings.TrimPrefix(filepath.Ext(file), ".")

				fmt.Printf("[2/4] Submitting %s as %s...\n", file, slug)
				sub, err := b.Submit(ctx, ref, slug, file)
				if err != nil {
					return err
				}
				fmt.Printf("      submission %s\n", sub.GlobalID)

				// The submission now exists on CodeRun's servers. Every exit
				// path from here must leave the user able to find it again.
				attempt, err := st.NextAttempt(ctx, ref.SelectionSlug, ref.ProblemSlug)
				if err != nil {
					// No attempt number means no artifact filename. The globalId
					// goes into the error text, because it is the only handle
					// that can recover this submission.
					return fmt.Errorf("submission %s was accepted by CodeRun but no attempt number could be reserved: %w",
						sub.GlobalID, err)
				}

				record := func(verdict string) error {
					return storage.WriteAttempt("solutions", ref, attempt, ext, source, storage.AttemptMeta{
						Language:     slug,
						SubmissionID: sub.GlobalID,
						Verdict:      verdict,
					})
				}

				fmt.Println("[3/4] Waiting for the verdict...")
				final, verdictErr := b.AwaitVerdict(ctx, sub.GlobalID, cfg.PollInterval, cfg.SubmissionTimeout)
				if verdictErr != nil {
					// Record what we know before surfacing the failure. The
					// globalId is the only handle that can recover this
					// submission later.
					if err := record("UNKNOWN"); err != nil {
						return fmt.Errorf("could not read the verdict (%v), and recording the attempt also failed: %w", verdictErr, err)
					}
					return fmt.Errorf("submission %s saved as attempt %d, but the verdict could not be read: %w",
						sub.GlobalID, attempt, verdictErr)
				}
				final.Ref = ref

				fmt.Println("[4/4] Recording the attempt...")
				if err := record(final.Verdict); err != nil {
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
