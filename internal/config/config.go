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
		BrowserProfileDir: envStr("BROWSER_PROFILE_DIR", ".browser"),
		DBPath:            envStr("DB_PATH", "data/coderun.db"),
	}

	var err error
	if cfg.Headless, err = envBool("HEADLESS", false); err != nil {
		return nil, err
	}
	if cfg.MaxConcurrentJobs, err = envInt("MAX_CONCURRENT_JOBS", 1); err != nil {
		return nil, err
	}
	maxSourceBytes, err := envInt("MAX_SOURCE_BYTES", 262144)
	if err != nil {
		return nil, err
	}
	cfg.MaxSourceBytes = int64(maxSourceBytes)
	maxArtifactBytes, err := envInt("MAX_ARTIFACT_BYTES", 262144)
	if err != nil {
		return nil, err
	}
	cfg.MaxArtifactBytes = int64(maxArtifactBytes)

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
	if cfg.MaxSourceBytes <= 0 {
		return nil, fmt.Errorf("MAX_SOURCE_BYTES must be positive, got %d", cfg.MaxSourceBytes)
	}
	if cfg.MaxSourceBytes > 262144 {
		return nil, fmt.Errorf("MAX_SOURCE_BYTES must not exceed 262144 (CodeRun's limit), got %d", cfg.MaxSourceBytes)
	}
	if cfg.MaxArtifactBytes <= 0 {
		return nil, fmt.Errorf("MAX_ARTIFACT_BYTES must be positive, got %d", cfg.MaxArtifactBytes)
	}
	return cfg, nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool and envInt fail loudly on an unparseable value rather than quietly
// falling back to the default: a malformed HEADLESS=ture must not silently
// run headed, and a malformed byte limit must not silently keep a default
// that may not be what the operator intended.
func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
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
