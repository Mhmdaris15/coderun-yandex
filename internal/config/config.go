// Package config loads agent settings from the environment.
//
// Secrets never live here. The browser session is stored on disk under
// BrowserProfileDir; no password or token is ever read from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// BaseURL is the only place the CodeRun origin is written down.
const BaseURL = "https://coderun.yandex.ru"

type Config struct {
	Headless          bool
	BrowserProfileDir string
	RequestDelay      time.Duration
	PollInterval      time.Duration
	SubmissionTimeout time.Duration
	MaxConcurrentJobs int
	DBPath            string
	MaxSourceBytes    int64
	MaxArtifactBytes  int64
}

func Load() (*Config, error) {
	if os.Getenv("CODERUN_SKIP_DOTENV") == "" {
		// A missing .env is normal, not an error.
		_ = godotenv.Load()
	}

	cfg := &Config{
		Headless:          envBool("HEADLESS", false),
		BrowserProfileDir: envStr("BROWSER_PROFILE_DIR", ".browser"),
		MaxConcurrentJobs: envInt("MAX_CONCURRENT_JOBS", 1),
		DBPath:            envStr("DB_PATH", "data/coderun.db"),
		MaxSourceBytes:    int64(envInt("MAX_SOURCE_BYTES", 262144)),
		MaxArtifactBytes:  int64(envInt("MAX_ARTIFACT_BYTES", 262144)),
	}

	var err error
	if cfg.RequestDelay, err = envDur("REQUEST_DELAY", time.Second); err != nil {
		return nil, err
	}
	if cfg.PollInterval, err = envDur("POLL_INTERVAL", 2*time.Second); err != nil {
		return nil, err
	}
	if cfg.SubmissionTimeout, err = envDur("SUBMISSION_TIMEOUT", 120*time.Second); err != nil {
		return nil, err
	}

	// Sequential-only is a safety property, not a tuning knob. Refuse to start
	// rather than silently ignoring a value the operator clearly meant.
	if cfg.MaxConcurrentJobs != 1 {
		return nil, fmt.Errorf("MAX_CONCURRENT_JOBS must be 1 in this milestone, got %d", cfg.MaxConcurrentJobs)
	}
	if cfg.MaxSourceBytes > 262144 {
		return nil, fmt.Errorf("MAX_SOURCE_BYTES must not exceed 262144 (CodeRun's limit), got %d", cfg.MaxSourceBytes)
	}
	return cfg, nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDur(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
