package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func selectionsCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "selections",
		Short: "List CodeRun selections",
		RunE: func(_ *cobra.Command, _ []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				sels, err := b.ListSelections(ctx, group)
				if err != nil {
					return err
				}
				if err := st.UpsertSelections(ctx, sels); err != nil {
					return err
				}
				for i, s := range sels {
					fmt.Printf("%2d. %-34s %s\n", i+1, s.Slug, s.Title)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "coderun-seasons", "selection group")
	return cmd
}

func problemsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "problems <selection>",
		Short: "List a selection's problems across all pages",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				probs, err := b.ListProblems(ctx, args[0])
				if err != nil {
					return err
				}
				if err := st.UpsertProblems(ctx, probs); err != nil {
					return err
				}
				for _, p := range probs {
					fmt.Printf("%3d. %-40s %-10s %s\n", p.Number, p.Ref.ProblemSlug, p.Difficulty.Raw, p.Title)
				}
				fmt.Printf("\n%d problems\n", len(probs))
				return nil
			})
		},
	}
}

func problemCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "problem <selection> <slug>",
		Short: "Fetch and display one problem",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				ref := coderun.ProblemRef{SelectionSlug: args[0], ProblemSlug: args[1]}

				p, contextID, err := b.GetProblem(ctx, ref)
				if err != nil {
					return err
				}
				if err := st.SaveProblem(ctx, p); err != nil {
					return err
				}
				if contextID != 0 {
					if err := st.SetContextID(ctx, p.Ref); err != nil {
						return err
					}
				}

				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				}
				fmt.Printf("%d. %s  [%s]\n\n%s\n\n", p.Number, p.Title, p.Difficulty.Raw, p.Statement)
				fmt.Printf("Формат ввода\n%s\n\nФормат вывода\n%s\n", p.InputFormat, p.OutputFormat)
				for i, ex := range p.Examples {
					fmt.Printf("\nExample %d\nInput:\n%s\nOutput:\n%s\n", i+1, ex.Input, ex.Output)
				}
				fmt.Printf("\nLanguages: %d\n", len(p.Languages))
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full problem as JSON")
	return cmd
}
