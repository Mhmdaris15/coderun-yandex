// Command coderun-agent automates Yandex CodeRun through a real browser.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

// Compile-time proof that the browser implementation satisfies the interface.
var _ coderun.CodeRunClient = (*pwclient.Browser)(nil)

var verbose bool

func main() {
	root := &cobra.Command{
		Use:   "coderun-agent",
		Short: "Automate Yandex CodeRun through a real browser",
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
				&slog.HandlerOptions{Level: level})))
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "debug logging")

	root.AddCommand(authCmd(), selectionsCmd(), problemsCmd(), problemCmd(),
		submitCmd(), statusCmd(), notImplemented("solve", "generate and submit a solution"),
		notImplemented("solution", "show the accepted solution"))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// notImplemented registers commands that belong to the LLM milestone, so the
// CLI surface is honest about what exists.
func notImplemented(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short + " (not implemented in this milestone)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%s is not implemented in this milestone", name)
		},
	}
}

// withBrowser wires config, storage and browser, and guarantees cleanup.
func withBrowser(headed bool, fn func(ctx context.Context, cfg *config.Config, b *pwclient.Browser, st *storage.Store) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	b, err := pwclient.New(cfg, headed)
	if err != nil {
		return err
	}
	defer b.Close()

	return fn(context.Background(), cfg, b, st)
}

// resolveCompiler turns a user-supplied language name into a compilerSlug
// using the list scraped from the page. Slugs are never derived from names:
// JavaScript's is nodejs_20_make.
func resolveCompiler(name string, compilers []coderun.Compiler) (string, error) {
	want := strings.ToLower(strings.TrimSpace(name))

	for _, c := range compilers {
		if strings.ToLower(c.Slug) == want {
			return c.Slug, nil
		}
	}
	for _, c := range compilers {
		if strings.ToLower(c.Title) == want {
			return c.Slug, nil
		}
	}
	// Last resort: a prefix match, so "pyth" finds "Python".
	//
	// An ambiguous prefix is an error, never a silent pick. "jav" prefixes both
	// Java and JavaScript, and quietly choosing whichever the site happened to
	// list first would submit the wrong language — which still counts as a real
	// submission against the user's record.
	if want != "" {
		var matches []coderun.Compiler
		for _, c := range compilers {
			if strings.HasPrefix(strings.ToLower(c.Title), want) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 1 {
			return matches[0].Slug, nil
		}
		if len(matches) > 1 {
			var names []string
			for _, c := range matches {
				names = append(names, fmt.Sprintf("%s (%s)", c.Slug, c.Title))
			}
			return "", fmt.Errorf("language %q is ambiguous; did you mean one of: %s",
				name, strings.Join(names, ", "))
		}
	}

	var available []string
	for _, c := range compilers {
		available = append(available, fmt.Sprintf("%s (%s)", c.Slug, c.Title))
	}
	return "", fmt.Errorf("unknown language %q; available: %s", name, strings.Join(available, ", "))
}
