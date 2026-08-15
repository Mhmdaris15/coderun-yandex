package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"coderun-agent/internal/coderun"
)

// AttemptMeta is the sidecar recorded next to each submitted source file. It
// exists so a human can later reconstruct what was tried and what happened.
type AttemptMeta struct {
	SelectionSlug string    `json:"selection_slug"`
	ProblemSlug   string    `json:"problem_slug"`
	Attempt       int       `json:"attempt"`
	Language      string    `json:"language"`
	SubmissionID  string    `json:"submission_id,omitempty"`
	Verdict       string    `json:"verdict,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// WriteAttempt records one submission attempt. Existing attempts are never
// overwritten: the point of numbering them is to keep the whole history.
func WriteAttempt(root string, ref coderun.ProblemRef, attempt int, ext string, source []byte, meta AttemptMeta) error {
	dir := filepath.Join(root, ref.SelectionSlug, ref.ProblemSlug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create attempt directory: %w", err)
	}

	base := fmt.Sprintf("attempt-%02d", attempt)
	srcPath := filepath.Join(dir, base+"."+ext)
	metaPath := filepath.Join(dir, base+".json")

	if _, err := os.Stat(srcPath); err == nil {
		return fmt.Errorf("%s already exists: attempts are immutable", srcPath)
	}

	meta.SelectionSlug = ref.SelectionSlug
	meta.ProblemSlug = ref.ProblemSlug
	meta.Attempt = attempt
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}

	if err := os.WriteFile(srcPath, source, 0o644); err != nil {
		return fmt.Errorf("write source: %w", err)
	}
	blob, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, blob, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}
