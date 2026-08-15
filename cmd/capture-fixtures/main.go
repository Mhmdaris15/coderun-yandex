// Command capture-fixtures saves real CodeRun pages into the extract
// package's testdata directory.
//
// Capture runs through a headless browser because problem statements are
// rendered after hydration: a plain HTTP GET returns the page shell without
// the statement body, its KaTeX, or its examples. Listings would survive a
// raw fetch, but using one mechanism for all three keeps the fixtures
// consistent with what the real crawler sees.
//
// These pages are public; no authentication is involved. Run this
// deliberately when refreshing fixtures — it is not part of the test suite.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mxschmitt/playwright-go"

	"coderun-agent/internal/config"
)

type target struct {
	name string
	path string
	// mustContain is a substring that proves the page finished rendering.
	// Capturing a shell without it would silently produce useless fixtures.
	mustContain string
}

var targets = []target{
	{"selections.html", "/selections?group=coderun-seasons", "CodeRun Boost Challenge"},
	{"selection-2025-summer-common.html", "/selections/2025-summer-common", "problem-list-item"},
	{"problem-bridge-to-the-palace.html", "/selections/2025-summer-common/problems/bridge-to-the-palace", "Формат ввода"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	outDir := filepath.Join("internal", "coderun", "extract", "testdata")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("start playwright (did you run `playwright install chromium`?): %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("launch chromium: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return fmt.Errorf("open page: %w", err)
	}

	for _, t := range targets {
		// One second between navigations. We are a guest on this site.
		time.Sleep(time.Second)

		html, err := capture(page, t)
		if err != nil {
			return err
		}
		dest := filepath.Join(outDir, t.name)
		if err := os.WriteFile(dest, []byte(html), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("saved %s (%d bytes)\n", dest, len(html))
	}
	return nil
}

func capture(page playwright.Page, t target) (string, error) {
	if _, err := page.Goto(config.BaseURL+t.path, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return "", fmt.Errorf("navigate %s: %w", t.path, err)
	}

	// Wait for the marker that proves rendering completed, rather than
	// trusting networkidle alone.
	if _, err := page.WaitForFunction(
		fmt.Sprintf("() => document.documentElement.outerHTML.includes(%q)", t.mustContain),
		nil,
		playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(20000)},
	); err != nil {
		return "", fmt.Errorf("%s never rendered %q: %w", t.name, t.mustContain, err)
	}

	html, err := page.Content()
	if err != nil {
		return "", fmt.Errorf("read content for %s: %w", t.name, err)
	}
	return html, nil
}
