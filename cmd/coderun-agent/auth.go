package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage the browser session"}

	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Open a browser and wait for you to log into Yandex",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Always headed: logging in is inherently interactive.
			return withBrowser(true, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, _ *storage.Store) error {
				return b.AwaitLogin(ctx, 5*time.Minute)
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report whether the stored session is still valid",
		RunE: func(_ *cobra.Command, _ []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, _ *storage.Store) error {
				ok, err := b.AuthStatus(ctx)
				if err != nil {
					return err
				}
				if ok {
					fmt.Println("authenticated")
					return nil
				}
				return fmt.Errorf("not authenticated — run `coderun-agent auth login`")
			})
		},
	})

	return cmd
}
