package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"coderun-agent/internal/coderun"
)

// AttemptMeta is the sidecar recorded next to each submitted source file. It
// exists so a human can later reconstruct what was tried and what happened.
type AttemptMeta struct {
	Attempt  int    `json:"attempt"`
	Language string `json:"language"`
	Verdict  string `json:"verdict"`
}

// WriteAttempt records one submission attempt. Existing attempts are never
// overwritten: the point of numbering them is to keep the whole history.
func WriteAttempt(root string, ref coderun.ProblemRef, attempt int, ext string, source []byte, meta AttemptMeta) error {
	dir := filepath.Join(root, ref.SelectionSlug, ref.ProblemSlug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	sourceFile := filepath.Join(dir, fmt.Sprintf("attempt-%02d.%s", attempt, ext))
	metaFile := filepath.Join(dir, fmt.Sprintf("attempt-%02d.json", attempt))

	if _, err := os.Stat(sourceFile); err == nil {
		return fmt.Errorf("attempt %d already exists; refusing to overwrite", attempt)
	}
	if _, err := os.Stat(metaFile); err == nil {
		return fmt.Errorf("attempt %d already exists; refusing to overwrite", attempt)
	}

	if err := os.WriteFile(sourceFile, source, 0644); err != nil {
		return fmt.Errorf("failed to write source file: %w", err)
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaFile, metaBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}
