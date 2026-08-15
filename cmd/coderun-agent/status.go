package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show locally recorded progress",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			st, err := storage.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()

			counts, err := st.Counts(context.Background())
			if err != nil {
				return err
			}
			total := 0
			for _, n := range counts {
				total += n
			}
			fmt.Printf("Total:      %d\n", total)
			fmt.Printf("Accepted:   %d\n", counts[coderun.StatusSolved])
			fmt.Printf("Attempted:  %d\n", counts[coderun.StatusWrong])
			fmt.Printf("Unsolved:   %d\n", counts[coderun.StatusNotSolved])
			return nil
		},
	}
}
