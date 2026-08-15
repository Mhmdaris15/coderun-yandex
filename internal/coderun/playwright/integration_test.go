//go:build integration

// These tests talk to the real coderun.yandex.ru. Run them deliberately:
//
//	go test -tags integration ./internal/coderun/playwright/ -v
//
// Crawl tests need no authentication. TestIntegrationAuthStatus does, and
// expects a session created by `coderun-agent auth login`.
package pwclient

import (
	"context"
	"testing"

	"coderun-agent/internal/config"
)

func newBrowser(t *testing.T) *Browser {
	t.Helper()
	t.Setenv("CODERUN_SKIP_DOTENV", "1")
	t.Setenv("HEADLESS", "true")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func TestIntegrationListSelections(t *testing.T) {
	b := newBrowser(t)

	sels, err := b.ListSelections(context.Background(), "coderun-seasons")
	if err != nil {
		t.Fatal(err)
	}
	if len(sels) < 10 {
		t.Errorf("got %d selections, expected at least 10", len(sels))
	}
	t.Logf("found %d selections", len(sels))
}

func TestIntegrationListProblemsPaginates(t *testing.T) {
	b := newBrowser(t)

	probs, err := b.ListProblems(context.Background(), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	// Page one holds 20. Anything more proves pagination worked.
	if len(probs) <= 20 {
		t.Errorf("got %d problems; expected more than one page's worth", len(probs))
	}
	t.Logf("found %d problems", len(probs))
}

func TestIntegrationGetProblem(t *testing.T) {
	b := newBrowser(t)

	ref := ProblemRefFor("2025-summer-common", "bridge-to-the-palace")
	p, contextID, err := b.GetProblem(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Мост во дворец" {
		t.Errorf("Title = %q", p.Title)
	}
	if contextID == 0 {
		t.Error("contextID not captured: the solution-template request was not observed")
	}
	if len(p.Examples) != 2 {
		t.Errorf("got %d examples, want 2", len(p.Examples))
	}
	t.Logf("contextID=%d languages=%d", contextID, len(p.Languages))
}

func TestIntegrationAuthStatus(t *testing.T) {
	b := newBrowser(t)

	ok, err := b.AuthStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skip("not authenticated; run `coderun-agent auth login` to exercise this test")
	}
}
