// Package pwclient drives CodeRun through a real browser.
//
// One persistent context, one page, sequential navigation. The package never
// attempts to evade anti-bot measures: if the site challenges us, the run
// stops and reports.
package pwclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"coderun-agent/internal/config"
)

// ErrChallenge means the site asked for human verification, or the session
// expired. Callers must surface it, never retry through it.
var ErrChallenge = errors.New("coderun presented a challenge")

type Browser struct {
	pw          *playwright.Playwright
	Ctx         playwright.BrowserContext
	Page        playwright.Page
	delay       time.Duration
	maxSource   int64
	maxArtifact int64
}

// New launches a persistent browser context. headedOverride forces a visible
// browser regardless of config, which `auth login` uses because logging in is
// inherently interactive.
func New(cfg *config.Config, headedOverride bool) (*Browser, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright (did you run `playwright install chromium`?): %w", err)
	}

	headless := cfg.Headless && !headedOverride

	ctx, err := pw.Chromium.LaunchPersistentContext(cfg.BrowserProfileDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(headless),
		})
	if err != nil {
		pw.Stop()
		return nil, fmt.Errorf("launch persistent context at %s: %w", cfg.BrowserProfileDir, err)
	}

	// A persistent context opens with one page already present.
	var page playwright.Page
	if pages := ctx.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = ctx.NewPage(); err != nil {
		ctx.Close()
		pw.Stop()
		return nil, fmt.Errorf("open page: %w", err)
	}

	return &Browser{
		pw:          pw,
		Ctx:         ctx,
		Page:        page,
		delay:       cfg.RequestDelay,
		maxSource:   cfg.MaxSourceBytes,
		maxArtifact: cfg.MaxArtifactBytes,
	}, nil
}

func (b *Browser) Close() error {
	var firstErr error
	if err := b.Ctx.Close(); err != nil {
		firstErr = err
	}
	if err := b.pw.Stop(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Goto navigates to a site-relative path and returns the page HTML. It applies
// the configured request delay before navigating and checks for challenges
// after, so every caller inherits both behaviours.
func (b *Browser) Goto(ctx context.Context, path string) (string, error) {
	return b.gotoOn(ctx, b.Page, path)
}

// gotoOn is Goto parameterised over the page to navigate. It exists so a
// caller that must not disturb the main page — AwaitLogin's polling, in
// particular, which must never navigate the operator's login form out from
// under them — can drive a separate page through the same delay and
// challenge-detection behaviour.
func (b *Browser) gotoOn(ctx context.Context, page playwright.Page, path string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(b.delay):
	}

	url := config.BaseURL + path
	slog.Debug("navigating", "path", path)

	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return "", fmt.Errorf("navigate to %s: %w", path, err)
	}

	html, err := page.Content()
	if err != nil {
		return "", fmt.Errorf("read page content: %w", err)
	}
	if err := DetectChallenge(page.URL(), html); err != nil {
		return "", err
	}
	return html, nil
}

// challengeMarkers are substrings that indicate a verification page rather
// than content. Kept deliberately narrow to avoid false positives.
var challengeMarkers = []string{
	"CheckboxCaptcha",
	"SmartCaptcha",
	"Подтвердите, что запросы отправляли вы",
}

// DetectChallenge reports whether a loaded page is a verification wall or an
// unexpected logout. Pure, so it is unit-tested without a browser.
func DetectChallenge(url, html string) error {
	if strings.Contains(url, "passport.yandex.ru") {
		return fmt.Errorf("%w: redirected to %s — run `coderun-agent auth login`", ErrChallenge, "passport.yandex.ru")
	}
	if strings.Contains(url, "showcaptcha") {
		return fmt.Errorf("%w: captcha at %s — complete it manually in the browser", ErrChallenge, url)
	}
	for _, marker := range challengeMarkers {
		if strings.Contains(html, marker) {
			return fmt.Errorf("%w: verification page detected at %s", ErrChallenge, url)
		}
	}
	return nil
}
