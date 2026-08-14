package storage

import (
	"os"
	"path/filepath"
	"testing"

	"coderun-agent/internal/coderun"
)

func TestWriteAttemptCreatesSourceAndMetadata(t *testing.T) {
	root := t.TempDir()
	ref := coderun.ProblemRef{SelectionSlug: "2025-summer-common", ProblemSlug: "bridge-to-the-palace"}

	err := WriteAttempt(root, ref, 1, "py", []byte("def solution(n, a):\n    return 0\n"),
		AttemptMeta{Attempt: 1, Language: "python_make", Verdict: "WRONG_ANSWER"})
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "2025-summer-common", "bridge-to-the-palace")
	for _, name := range []string{"attempt-01.py", "attempt-01.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestWriteAttemptZeroPadsAndDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	ref := coderun.ProblemRef{SelectionSlug: "s", ProblemSlug: "p"}

	if err := WriteAttempt(root, ref, 1, "py", []byte("first"), AttemptMeta{Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteAttempt(root, ref, 2, "py", []byte("second"), AttemptMeta{Attempt: 2}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "s", "p")
	first, err := os.ReadFile(filepath.Join(dir, "attempt-01.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "first" {
		t.Errorf("attempt-01.py = %q, want %q — earlier attempts must never be overwritten", first, "first")
	}
	if _, err := os.Stat(filepath.Join(dir, "attempt-02.py")); err != nil {
		t.Errorf("attempt-02.py missing: %v", err)
	}
}

func TestWriteAttemptRefusesToClobber(t *testing.T) {
	root := t.TempDir()
	ref := coderun.ProblemRef{SelectionSlug: "s", ProblemSlug: "p"}

	if err := WriteAttempt(root, ref, 1, "py", []byte("a"), AttemptMeta{Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteAttempt(root, ref, 1, "py", []byte("b"), AttemptMeta{Attempt: 1}); err == nil {
		t.Fatal("expected an error rather than silently overwriting an existing attempt")
	}
}
