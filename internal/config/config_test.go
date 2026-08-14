package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("CODERUN_SKIP_DOTENV", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Headless {
		t.Error("Headless should default to false so the operator can watch the browser")
	}
	if cfg.BrowserProfileDir != ".browser" {
		t.Errorf("BrowserProfileDir = %q, want %q", cfg.BrowserProfileDir, ".browser")
	}
	if cfg.PollInterval != 2*time.Second {
		t.Errorf("PollInterval = %v, want 2s", cfg.PollInterval)
	}
	if cfg.SubmissionTimeout != 120*time.Second {
		t.Errorf("SubmissionTimeout = %v, want 120s", cfg.SubmissionTimeout)
	}
	if cfg.MaxSourceBytes != 262144 {
		t.Errorf("MaxSourceBytes = %d, want 262144", cfg.MaxSourceBytes)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("CODERUN_SKIP_DOTENV", "1")
	t.Setenv("HEADLESS", "true")
	t.Setenv("POLL_INTERVAL", "5s")
	t.Setenv("DB_PATH", "/tmp/x.db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Headless {
		t.Error("HEADLESS=true should set Headless")
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", cfg.PollInterval)
	}
	if cfg.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
}

func TestLoadRejectsConcurrency(t *testing.T) {
	t.Setenv("CODERUN_SKIP_DOTENV", "1")
	t.Setenv("MAX_CONCURRENT_JOBS", "4")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error: this milestone is sequential-only")
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Setenv("CODERUN_SKIP_DOTENV", "1")
	t.Setenv("POLL_INTERVAL", "soon")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an unparseable duration")
	}
}
