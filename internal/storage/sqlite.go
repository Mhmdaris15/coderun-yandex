// Package storage persists agent state.
//
// SQLite is the single source of truth: if the program reads it, it lives
// here. Files written under solutions/ are artifacts for humans and are never
// read back.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo

	"coderun-agent/internal/coderun"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS selections (
    slug          TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    grp           TEXT NOT NULL DEFAULT '',
    problem_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS problems (
    selection_slug TEXT NOT NULL,
    problem_slug   TEXT NOT NULL,
    context_id     INTEGER,
    number         INTEGER NOT NULL DEFAULT 0,
    title          TEXT NOT NULL DEFAULT '',
    difficulty_raw TEXT NOT NULL DEFAULT '',
    difficulty     TEXT NOT NULL DEFAULT 'UNKNOWN',
    status         TEXT NOT NULL DEFAULT 'UNKNOWN',
    statement_json TEXT,
    fetched_at     TIMESTAMP,
    PRIMARY KEY (selection_slug, problem_slug)
);

CREATE TABLE IF NOT EXISTS attempts (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_slug TEXT NOT NULL,
    problem_slug   TEXT NOT NULL,
    attempt        INTEGER NOT NULL,
    compiler_slug  TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL,
    UNIQUE (selection_slug, problem_slug, attempt)
);

CREATE TABLE IF NOT EXISTS submissions (
    global_id       TEXT PRIMARY KEY,
    selection_slug  TEXT NOT NULL,
    problem_slug    TEXT NOT NULL,
    attempt         INTEGER NOT NULL DEFAULT 0,
    compiler_slug   TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT '',
    verdict         TEXT NOT NULL DEFAULT '',
    accepted        INTEGER NOT NULL DEFAULT 0,
    max_time_ms     INTEGER NOT NULL DEFAULT 0,
    max_memory_bytes INTEGER NOT NULL DEFAULT 0,
    first_failed_test INTEGER NOT NULL DEFAULT 0,
    compile_log     TEXT,
    submitted_at    TIMESTAMP
);

CREATE TABLE IF NOT EXISTS test_results (
    global_id    TEXT NOT NULL,
    test_number  INTEGER NOT NULL,
    verdict      TEXT NOT NULL DEFAULT '',
    is_sample    INTEGER NOT NULL DEFAULT 0,
    time_ms      INTEGER NOT NULL DEFAULT 0,
    memory_bytes INTEGER NOT NULL DEFAULT 0,
    input        TEXT,
    output       TEXT,
    answer       TEXT,
    PRIMARY KEY (global_id, test_number)
);
`

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Serialised access; this milestone is single-threaded by design.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) UpsertSelections(ctx context.Context, sels []coderun.Selection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO selections (slug, title, grp, problem_count)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(slug) DO UPDATE SET
            title = excluded.title,
            grp = excluded.grp,
            problem_count = excluded.problem_count`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sel := range sels {
		if _, err := stmt.ExecContext(ctx, sel.Slug, sel.Title, sel.Group, sel.ProblemCount); err != nil {
			return fmt.Errorf("upsert selection %s: %w", sel.Slug, err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListSelections(ctx context.Context) ([]coderun.Selection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slug, title, grp, problem_count FROM selections ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []coderun.Selection
	for rows.Next() {
		var sel coderun.Selection
		if err := rows.Scan(&sel.Slug, &sel.Title, &sel.Group, &sel.ProblemCount); err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, rows.Err()
}

// UpsertProblems preserves any context_id already discovered: listing pages do
// not carry it, so a naive overwrite would erase it on every re-crawl.
func (s *Store) UpsertProblems(ctx context.Context, probs []coderun.ProblemSummary) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO problems (selection_slug, problem_slug, number, title,
                              difficulty_raw, difficulty, status)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(selection_slug, problem_slug) DO UPDATE SET
            number = excluded.number,
            title = excluded.title,
            difficulty_raw = excluded.difficulty_raw,
            difficulty = excluded.difficulty,
            status = excluded.status`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range probs {
		if _, err := stmt.ExecContext(ctx,
			p.Ref.SelectionSlug, p.Ref.ProblemSlug, p.Number, p.Title,
			p.Difficulty.Raw, string(p.Difficulty.Level), string(p.Status),
		); err != nil {
			return fmt.Errorf("upsert problem %s: %w", p.Ref.ProblemSlug, err)
		}
	}
	return tx.Commit()
}

func (s *Store) SetContextID(ctx context.Context, ref coderun.ProblemRef) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE problems SET context_id = ? WHERE selection_slug = ? AND problem_slug = ?`,
		ref.ContextID, ref.SelectionSlug, ref.ProblemSlug)
	return err
}

func (s *Store) GetContextID(ctx context.Context, selectionSlug, problemSlug string) (int, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT context_id FROM problems WHERE selection_slug = ? AND problem_slug = ?`,
		selectionSlug, problemSlug).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("context id for %s/%s: %w", selectionSlug, problemSlug, err)
	}
	if !id.Valid {
		return 0, fmt.Errorf("context id for %s/%s not discovered yet", selectionSlug, problemSlug)
	}
	return int(id.Int64), nil
}

func (s *Store) SaveProblem(ctx context.Context, p *coderun.Problem) error {
	blob, err := marshalProblem(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO problems (selection_slug, problem_slug, number, title,
                              difficulty_raw, difficulty, status, statement_json, fetched_at)
        VALUES (?, ?, ?, ?, ?, ?, 'UNKNOWN', ?, ?)
        ON CONFLICT(selection_slug, problem_slug) DO UPDATE SET
            number = excluded.number,
            title = excluded.title,
            difficulty_raw = excluded.difficulty_raw,
            difficulty = excluded.difficulty,
            statement_json = excluded.statement_json,
            fetched_at = excluded.fetched_at`,
		p.Ref.SelectionSlug, p.Ref.ProblemSlug, p.Number, p.Title,
		p.Difficulty.Raw, string(p.Difficulty.Level), string(blob), p.FetchedAt)
	return err
}

// NextAttempt reserves and returns the next attempt number for a problem.
func (s *Store) NextAttempt(ctx context.Context, selectionSlug, problemSlug string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt), 0) + 1 FROM attempts
         WHERE selection_slug = ? AND problem_slug = ?`,
		selectionSlug, problemSlug).Scan(&next); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO attempts (selection_slug, problem_slug, attempt, created_at)
         VALUES (?, ?, ?, ?)`,
		selectionSlug, problemSlug, next, time.Now().UTC()); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// SaveSubmission upserts one submission row, keyed by global_id. Re-running
// the same submission (e.g. re-recording after a verdict finishes) is safe.
func (s *Store) SaveSubmission(ctx context.Context, sub *coderun.Submission, attempt int) error {
	accepted := 0
	if coderun.IsAccepted(sub.Verdict) {
		accepted = 1
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO submissions (global_id, selection_slug, problem_slug, attempt,
                                  compiler_slug, status, verdict, accepted,
                                  max_time_ms, max_memory_bytes, first_failed_test,
                                  compile_log, submitted_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(global_id) DO UPDATE SET
            selection_slug = excluded.selection_slug,
            problem_slug = excluded.problem_slug,
            attempt = excluded.attempt,
            compiler_slug = excluded.compiler_slug,
            status = excluded.status,
            verdict = excluded.verdict,
            accepted = excluded.accepted,
            max_time_ms = excluded.max_time_ms,
            max_memory_bytes = excluded.max_memory_bytes,
            first_failed_test = excluded.first_failed_test,
            compile_log = excluded.compile_log,
            submitted_at = excluded.submitted_at`,
		sub.GlobalID, sub.Ref.SelectionSlug, sub.Ref.ProblemSlug, attempt,
		sub.CompilerSlug, sub.Status, sub.Verdict, accepted,
		sub.MaxTimeMillis, sub.MaxMemoryBytes, sub.FirstFailedTest,
		sub.CompileLog, sub.SubmittedAt)
	if err != nil {
		return fmt.Errorf("save submission %s: %w", sub.GlobalID, err)
	}
	return nil
}

// SaveTestResults replaces the stored per-test rows for a submission: existing
// rows for global_id are cleared first so re-saving never duplicates a test.
func (s *Store) SaveTestResults(ctx context.Context, globalID string, tests []coderun.TestResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM test_results WHERE global_id = ?`, globalID); err != nil {
		return fmt.Errorf("clear test results for %s: %w", globalID, err)
	}

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO test_results (global_id, test_number, verdict, is_sample,
                                   time_ms, memory_bytes, input, output, answer)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tests {
		isSample := 0
		if t.IsSample {
			isSample = 1
		}
		if _, err := stmt.ExecContext(ctx, globalID, t.Number, t.Verdict, isSample,
			t.TimeMillis, t.MemoryBytes, t.Input, t.Output, t.Answer); err != nil {
			return fmt.Errorf("save test %d for %s: %w", t.Number, globalID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) Counts(ctx context.Context) (map[coderun.Status]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM problems GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[coderun.Status]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[coderun.Status(status)] = n
	}
	return out, rows.Err()
}
