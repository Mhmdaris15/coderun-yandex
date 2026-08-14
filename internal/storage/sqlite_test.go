package storage

import (
	"context"
	"path/filepath"
	"testing"

	"coderun-agent/internal/coderun"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertSelectionsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	sels := []coderun.Selection{
		{Slug: "2025-summer-common", Title: "CodeRun Boost Challenge"},
		{Slug: "2026-summer-common", Title: "CodeRun Summer Challenge"},
	}
	for i := 0; i < 2; i++ {
		if err := s.UpsertSelections(ctx, sels); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}

	got, err := s.ListSelections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d selections after two upserts, want 2", len(got))
	}
}

func TestUpsertProblemsUpdatesStatus(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	ref := coderun.ProblemRef{SelectionSlug: "sel", ProblemSlug: "p1"}
	if err := s.UpsertProblems(ctx, []coderun.ProblemSummary{
		{Ref: ref, Number: 1, Title: "T", Status: coderun.StatusNotSolved},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProblems(ctx, []coderun.ProblemSummary{
		{Ref: ref, Number: 1, Title: "T", Status: coderun.StatusWrong},
	}); err != nil {
		t.Fatal(err)
	}

	counts, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[coderun.StatusWrong] != 1 {
		t.Errorf("Wrong count = %d, want 1", counts[coderun.StatusWrong])
	}
	if counts[coderun.StatusNotSolved] != 0 {
		t.Errorf("NotSolved count = %d, want 0 — the row should have been updated, not duplicated",
			counts[coderun.StatusNotSolved])
	}
}

func TestContextIDSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.db")

	ref := coderun.ProblemRef{SelectionSlug: "sel", ProblemSlug: "p1", ContextID: 1838}

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.UpsertProblems(ctx, []coderun.ProblemSummary{{Ref: ref, Number: 1, Title: "T"}}); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetContextID(ctx, ref); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// This is the resume-after-restart property.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.GetContextID(ctx, "sel", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1838 {
		t.Errorf("ContextID = %d, want 1838", got)
	}
}

func TestGetContextIDMissing(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetContextID(context.Background(), "nope", "nope"); err == nil {
		t.Fatal("expected an error for an unknown problem")
	}
}

func TestNextAttemptIncrements(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	for want := 1; want <= 3; want++ {
		got, err := s.NextAttempt(ctx, "sel", "p1")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("NextAttempt() = %d, want %d", got, want)
		}
	}
	// A different problem numbers independently.
	got, err := s.NextAttempt(ctx, "sel", "p2")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("NextAttempt(p2) = %d, want 1", got)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "x.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open should create missing parent directories: %v", err)
	}
	s.Close()
}
