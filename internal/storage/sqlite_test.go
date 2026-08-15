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

func TestSaveSubmissionRoundTripsAcceptedFlag(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	ref := coderun.ProblemRef{SelectionSlug: "sel", ProblemSlug: "p1"}

	cases := []struct {
		globalID string
		verdict  string
		accepted bool
	}{
		{"g-ok", "OK", true},
		{"g-wa", "WRONG_ANSWER", false},
	}
	for _, c := range cases {
		sub := &coderun.Submission{
			GlobalID:        c.globalID,
			Ref:             ref,
			CompilerSlug:    "python_make",
			Status:          "FINISHED",
			Verdict:         c.verdict,
			MaxTimeMillis:   42,
			MaxMemoryBytes:  1024,
			FirstFailedTest: 3,
			CompileLog:      "",
		}
		if err := s.SaveSubmission(ctx, sub, 1); err != nil {
			t.Fatalf("SaveSubmission(%s): %v", c.globalID, err)
		}
	}

	var gotVerdict string
	var gotAccepted int
	if err := s.db.QueryRowContext(ctx,
		`SELECT verdict, accepted FROM submissions WHERE global_id = ?`, "g-ok").
		Scan(&gotVerdict, &gotAccepted); err != nil {
		t.Fatal(err)
	}
	if gotVerdict != "OK" || gotAccepted != 1 {
		t.Errorf("accepted submission: verdict=%q accepted=%d, want OK/1", gotVerdict, gotAccepted)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT verdict, accepted FROM submissions WHERE global_id = ?`, "g-wa").
		Scan(&gotVerdict, &gotAccepted); err != nil {
		t.Fatal(err)
	}
	if gotVerdict != "WRONG_ANSWER" || gotAccepted != 0 {
		t.Errorf("rejected submission: verdict=%q accepted=%d, want WRONG_ANSWER/0", gotVerdict, gotAccepted)
	}
}

func TestSaveSubmissionUpsertIsSafeToRerun(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	ref := coderun.ProblemRef{SelectionSlug: "sel", ProblemSlug: "p1"}

	sub := &coderun.Submission{GlobalID: "g1", Ref: ref, Status: "PENDING", Verdict: ""}
	if err := s.SaveSubmission(ctx, sub, 1); err != nil {
		t.Fatal(err)
	}
	sub.Status = "FINISHED"
	sub.Verdict = "OK"
	if err := s.SaveSubmission(ctx, sub, 1); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM submissions WHERE global_id = ?`, "g1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d rows for g1, want 1 (re-saving must upsert, not duplicate)", count)
	}

	var verdict string
	if err := s.db.QueryRowContext(ctx, `SELECT verdict FROM submissions WHERE global_id = ?`, "g1").Scan(&verdict); err != nil {
		t.Fatal(err)
	}
	if verdict != "OK" {
		t.Errorf("verdict = %q, want OK (the second save should have updated the row)", verdict)
	}
}

func TestSaveTestResultsReplacesRatherThanDuplicates(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	tests1 := []coderun.TestResult{
		{Number: 1, Verdict: "WRONG_ANSWER", IsSample: true, TimeMillis: 10, MemoryBytes: 100, Input: "in1", Output: "out1", Answer: "ans1"},
		{Number: 2, Verdict: "OK", IsSample: true, TimeMillis: 5, MemoryBytes: 50},
	}
	if err := s.SaveTestResults(ctx, "g1", tests1); err != nil {
		t.Fatal(err)
	}

	// Re-save with a different, smaller set: the old rows must be gone, not
	// merely appended to.
	tests2 := []coderun.TestResult{
		{Number: 1, Verdict: "OK", IsSample: true, TimeMillis: 12, MemoryBytes: 110, Input: "in1b"},
	}
	if err := s.SaveTestResults(ctx, "g1", tests2); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_results WHERE global_id = ?`, "g1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d test rows for g1 after re-save, want 1 (replace, not accumulate)", count)
	}

	var verdict, input string
	if err := s.db.QueryRowContext(ctx,
		`SELECT verdict, input FROM test_results WHERE global_id = ? AND test_number = 1`, "g1").
		Scan(&verdict, &input); err != nil {
		t.Fatal(err)
	}
	if verdict != "OK" || input != "in1b" {
		t.Errorf("test 1 = (%q, %q), want (OK, in1b) from the second save", verdict, input)
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
