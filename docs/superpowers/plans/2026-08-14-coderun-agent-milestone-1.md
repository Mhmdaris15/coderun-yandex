# CodeRun Agent — Milestone 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that authenticates to Yandex CodeRun in a real browser, crawls selections/problems/statements, submits a manually supplied source file, and records the verdict with per-test detail.

**Architecture:** Browser-primary. A single Playwright persistent browser context is the only thing that talks to CodeRun. HTML is pulled from the page and handed to a pure `extract` package (no Playwright types in its signatures), which makes every parser testable offline against real captured fixtures. Verdict JSON is read through the browser context's own `APIRequestContext`, so there is no second HTTP stack and no cookie copying. SQLite is the single source of truth; files under `solutions/` are write-only artifacts for humans.

**Tech Stack:** Go 1.23+, `mxschmitt/playwright-go`, `PuerkitoBio/goquery`, `spf13/cobra`, `modernc.org/sqlite` (pure Go, no cgo), `joho/godotenv`, stdlib `log/slog` and `testing`.

> **Playwright import path.** Use `github.com/mxschmitt/playwright-go`, **not**
> `github.com/playwright-community/playwright-go`. The project moved to the
> `playwright-community` org, but as of v0.6201.0 the module's own `go.mod` still
> declares `module github.com/mxschmitt/playwright-go`, so Go refuses to import it
> under the community path. Verified against the module cache on 2026-08-14. This
> looks like a stale dependency and is not one — do not "fix" it.

**Source spec:** `docs/superpowers/specs/2026-08-14-coderun-agent-design.md`
**Field research:** `docs/coderun-research.md`

## Global Constraints

Every task's requirements implicitly include this section.

- Go 1.23 or newer. Module path is `coderun-agent`.
- **No cgo.** Use `modernc.org/sqlite` (driver name `"sqlite"`), never `mattn/go-sqlite3`.
- Base URL constant: `https://coderun.yandex.ru`. Never hardcode it anywhere else.
- **Never select on hashed CSS class names.** Only `data-testid`, ARIA roles, element `id`, href patterns, and heading structure. The one exception is the status token regex, which reads a class *fragment* by design.
- **Never log** cookies, session tokens, `x-csrf-token` values, or presigned S3 URLs. Presigned URLs log as the literal `<presigned>`.
- **No anti-bot evasion.** No user-agent spoofing, no stealth plugins, no proxy rotation. A CAPTCHA, a mid-run redirect to `passport.yandex.ru`, or a 403 aborts the run.
- Sequential only. One browser context, `MAX_CONCURRENT_JOBS=1`.
- Submitted source files are capped at 262144 bytes (256 KB) — CodeRun's own stated limit.
- Verdicts are an **open set**. `IsAccepted` returns true only for `"OK"` and `"ACCEPTED"`; everything else is not-accepted. Never write a closed `switch` over verdicts.
- Every function in `internal/coderun/extract/` takes strings and returns domain types. No `playwright.*` type may appear in that package.
- Russian UI strings are data, not identifiers. Match on class tokens and `data-testid` where available; where heading text must be matched (statement sections), keep the mapping in one table.

---

## File Structure

| File | Responsibility |
|---|---|
| `go.mod` | module + pinned deps |
| `.env.example` | non-secret config documentation |
| `internal/config/config.go` | env loading, defaults, validation |
| `internal/coderun/models.go` | domain types: `ProblemRef`, `Problem`, `Selection`, `Submission`, enums |
| `internal/coderun/client.go` | `CodeRunClient` interface |
| `internal/coderun/extract/status.go` | class token → `Status`, label → `Difficulty` |
| `internal/coderun/extract/katex.go` | KaTeX subtree → `$tex$` |
| `internal/coderun/extract/filters.go` | double-encoded `?filters=` blob |
| `internal/coderun/extract/listing.go` | selections list, problem list, pagination |
| `internal/coderun/extract/statement.go` | problem page → `Problem` |
| `internal/storage/sqlite.go` | schema, migrations, queries |
| `internal/storage/artifacts.go` | write-only `solutions/` files |
| `internal/coderun/playwright/browser.go` | context lifecycle, navigation, challenge detection |
| `internal/coderun/playwright/auth.go` | login, session validation |
| `internal/coderun/playwright/crawl.go` | `ListSelections`, `ListProblems`, `GetProblem`, compilers, template |
| `internal/coderun/playwright/submit.go` | file upload submission, `globalId` capture |
| `internal/coderun/playwright/verdict.go` | polling, verdict detail, test artifacts |
| `cmd/coderun-agent/main.go` | cobra root |
| `cmd/coderun-agent/*.go` | one file per command group |
| `cmd/capture-fixtures/main.go` | dev tool: save real HTML to `testdata/` |

---

## Task 1: Project scaffold and configuration

**Files:**
- Create: `go.mod`, `.env.example`, `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing (first task)
- Produces: `config.Config` struct and `config.Load() (*Config, error)`. Every later task reads settings from this struct. Field names and types are fixed here.

- [ ] **Step 1: Initialize the module**

```bash
cd "C:/Users/USER/Documents/Aris Project/coderun-yandex"
go mod init coderun-agent
go get github.com/joho/godotenv
```

- [ ] **Step 2: Write the failing test**

Create `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `undefined: Load`

- [ ] **Step 4: Write the implementation**

Create `internal/config/config.go`:

```go
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
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS, 4 tests

- [ ] **Step 6: Create `.env.example`**

```env
# Browser
HEADLESS=false
BROWSER_PROFILE_DIR=.browser

# Rate limiting — do not raise these
REQUEST_DELAY=1s
POLL_INTERVAL=2s
SUBMISSION_TIMEOUT=120s
MAX_CONCURRENT_JOBS=1

# Storage
DB_PATH=data/coderun.db

# Limits (bytes)
MAX_SOURCE_BYTES=262144
MAX_ARTIFACT_BYTES=262144

# No credentials belong in this file. Authentication uses a persistent
# browser profile created by `coderun-agent auth login`.
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum .env.example internal/config/
git commit -m "feat(config): add environment configuration with sequential-only guard"
```

---

## Task 2: Capture real HTML fixtures

Every parser in this plan is tested against **real** captured HTML, never hand-written approximations.

Capture goes through a headless browser, not `http.Get`. This was established empirically: a plain HTTP capture returns the problem page's shell (`problem-title`, the title text) but **not** its statement body, KaTeX, or `code-snippet` examples, which are rendered after hydration. Listings happen to survive a raw fetch; statements do not. Since the agent is browser-primary anyway, the capture tool uses the same mechanism the real crawler will.

This task therefore pulls the Playwright dependency and the Chromium download forward from Task 10. Crawling still needs no authentication.

**Files:**
- Create: `cmd/capture-fixtures/main.go`
- Create: `internal/coderun/extract/testdata/` (populated by running the tool)
- Test: `internal/coderun/extract/fixtures_test.go`

**Interfaces:**
- Consumes: `config.BaseURL`
- Produces: three fixture files every later `extract` task reads:
  - `testdata/selections.html` — `/selections?group=coderun-seasons`
  - `testdata/selection-2025-summer-common.html` — a selection's problem list
  - `testdata/problem-bridge-to-the-palace.html` — a problem page

- [ ] **Step 1: Add Playwright and install Chromium**

```bash
go get github.com/mxschmitt/playwright-go
go run github.com/mxschmitt/playwright-go/cmd/playwright@latest install chromium --with-deps
```

The install downloads a browser build and takes a few minutes.

Note the import path is `mxschmitt`, not `playwright-community` — see the Playwright
import path note under Tech Stack. This is deliberate and verified.

- [ ] **Step 2: Write the capture tool**

Create `cmd/capture-fixtures/main.go`:

```go
// Command capture-fixtures saves real CodeRun pages into the extract
// package's testdata directory.
//
// Capture runs through a headless browser because problem statements are
// rendered after hydration: a plain HTTP GET returns the page shell without
// the statement body, its KaTeX, or its examples. Listings would survive a
// raw fetch, but using one mechanism for all three keeps the fixtures
// consistent with what the real crawler sees.
//
// These pages are public; no authentication is involved. Run this
// deliberately when refreshing fixtures — it is not part of the test suite.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/playwright-community/playwright-go"

	"coderun-agent/internal/config"
)

type target struct {
	name string
	path string
	// mustContain is a substring that proves the page finished rendering.
	// Capturing a shell without it would silently produce useless fixtures.
	mustContain string
}

var targets = []target{
	{"selections.html", "/selections?group=coderun-seasons", "CodeRun Boost Challenge"},
	{"selection-2025-summer-common.html", "/selections/2025-summer-common", "problem-list-item"},
	{"problem-bridge-to-the-palace.html", "/selections/2025-summer-common/problems/bridge-to-the-palace", "Формат ввода"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	outDir := filepath.Join("internal", "coderun", "extract", "testdata")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("start playwright (did you run `playwright install chromium`?): %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("launch chromium: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return fmt.Errorf("open page: %w", err)
	}

	for _, t := range targets {
		// One second between navigations. We are a guest on this site.
		time.Sleep(time.Second)

		html, err := capture(page, t)
		if err != nil {
			return err
		}
		dest := filepath.Join(outDir, t.name)
		if err := os.WriteFile(dest, []byte(html), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("saved %s (%d bytes)\n", dest, len(html))
	}
	return nil
}

func capture(page playwright.Page, t target) (string, error) {
	if _, err := page.Goto(config.BaseURL+t.path, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return "", fmt.Errorf("navigate %s: %w", t.path, err)
	}

	// Wait for the marker that proves rendering completed, rather than
	// trusting networkidle alone.
	if _, err := page.WaitForFunction(
		fmt.Sprintf("() => document.documentElement.outerHTML.includes(%q)", t.mustContain),
		nil,
		playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(20000)},
	); err != nil {
		return "", fmt.Errorf("%s never rendered %q: %w", t.name, t.mustContain, err)
	}

	html, err := page.Content()
	if err != nil {
		return "", fmt.Errorf("read content for %s: %w", t.name, err)
	}
	return html, nil
}
```

- [ ] **Step 3: Run the capture tool**

Run: `go run ./cmd/capture-fixtures`
Expected: three `saved …` lines, each well over 20000 bytes.

If a `never rendered` error appears, the page structure changed — report it rather than removing the check.

- [ ] **Step 4: Write the fixture sanity test**

This test guards the assumption the whole `extract` package rests on: that the fixtures contain fully rendered content. If it fails, the fixtures are shells and every parser built on them would match nothing.

Create `internal/coderun/extract/fixtures_test.go`:

```go
package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads a captured page. Every extract test uses this.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("missing fixture %s: %v\n\nRun: go run ./cmd/capture-fixtures", name, err)
	}
	return string(b)
}

func TestFixturesAreServerRendered(t *testing.T) {
	cases := []struct {
		fixture string
		want    string
	}{
		{"selections.html", "CodeRun Boost Challenge"},
		{"selection-2025-summer-common.html", "problem-list-item"},
		{"problem-bridge-to-the-palace.html", "Формат ввода"},
		{"problem-bridge-to-the-palace.html", "problem-title"},
	}
	for _, tc := range cases {
		html := loadFixture(t, tc.fixture)
		if !strings.Contains(html, tc.want) {
			t.Errorf("%s does not contain %q — the page may not be server-rendered; "+
				"switch capture-fixtures to a browser-based capture", tc.fixture, tc.want)
		}
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -v`
Expected: PASS. If `Формат ввода` is missing, the capture did not wait long enough — fix the capture tool, never the assertion.

- [ ] **Step 6: Commit**

Fixtures are committed deliberately: they are the record of what the site actually looked like on this date.

```bash
git add cmd/capture-fixtures/ internal/coderun/extract/
git commit -m "test(extract): capture real CodeRun HTML fixtures"
```

---

## Task 3: Status and difficulty parsing

**Files:**
- Create: `internal/coderun/models.go`, `internal/coderun/extract/status.go`
- Test: `internal/coderun/extract/status_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `coderun.Status` (`StatusNotSolved`, `StatusWrong`, `StatusSolved`, `StatusUnknown`)
  - `coderun.Difficulty` struct `{Level DifficultyLevel; Raw string}`
  - `extract.ParseStatus(class string) coderun.Status`
  - `extract.ParseDifficulty(label string) coderun.Difficulty`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/extract/status_test.go`:

```go
package extract

import (
	"testing"

	"coderun-agent/internal/coderun"
)

func TestParseStatus(t *testing.T) {
	cases := []struct {
		name  string
		class string
		want  coderun.Status
	}{
		{"not solved", "ProblemStatusIcon_ProblemStatus__vf4en ProblemStatusIcon_ProblemStatus_type_not_solved__kcGLp ProblemListItem_Icon__n062g", coderun.StatusNotSolved},
		{"wrong", "ProblemStatusIcon_ProblemStatus_type_wrong__abc12", coderun.StatusWrong},
		{"solved", "ProblemStatusIcon_ProblemStatus_type_solved__xy890", coderun.StatusSolved},
		{"empty", "", coderun.StatusUnknown},
		{"unrecognised token", "ProblemStatusIcon_ProblemStatus_type_in_review__q1w2e", coderun.StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseStatus(tc.class); got != tc.want {
				t.Errorf("ParseStatus() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseDifficultyKeepsRaw(t *testing.T) {
	d := ParseDifficulty("Средняя")
	if d.Level != coderun.DifficultyMedium {
		t.Errorf("Level = %v, want Medium", d.Level)
	}
	if d.Raw != "Средняя" {
		t.Errorf("Raw = %q, want the original label preserved", d.Raw)
	}
}

func TestParseDifficultyUnknownPreservesLabel(t *testing.T) {
	// Only "Средняя" was observed in the wild. An unrecognised label must not
	// be lost or guessed at — it is preserved verbatim for later inspection.
	d := ParseDifficulty("Экстремальная")
	if d.Level != coderun.DifficultyUnknown {
		t.Errorf("Level = %v, want Unknown", d.Level)
	}
	if d.Raw != "Экстремальная" {
		t.Errorf("Raw = %q, want the original label preserved", d.Raw)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/extract/ -run 'Status|Difficulty' -v`
Expected: FAIL — `undefined: ParseStatus`

- [ ] **Step 3: Write the domain types**

Create `internal/coderun/models.go`:

```go
// Package coderun holds the domain model and the client interface.
package coderun

import "time"

// Status is a problem's solve state, parsed from the status icon's class token.
// Exactly three states exist on the site; see docs/coderun-research.md.
type Status string

const (
	StatusUnknown   Status = "UNKNOWN"
	StatusNotSolved Status = "NOT_SOLVED"
	StatusWrong     Status = "WRONG"
	StatusSolved    Status = "SOLVED"
)

type DifficultyLevel string

const (
	DifficultyUnknown DifficultyLevel = "UNKNOWN"
	DifficultyEasy    DifficultyLevel = "EASY"
	DifficultyMedium  DifficultyLevel = "MEDIUM"
	DifficultyHard    DifficultyLevel = "HARD"
)

// Difficulty keeps the original label alongside the parsed level. Only
// "Средняя" has been observed, so the raw string is the trustworthy field.
type Difficulty struct {
	Level DifficultyLevel
	Raw   string
}

// ProblemRef identifies a problem. The slug pair is the primary key; ContextID
// is discovered from the page's solution-template request and is required by
// the submission API.
type ProblemRef struct {
	SelectionSlug string
	ProblemSlug   string
	ContextID     int
}

type Selection struct {
	Slug         string
	Title        string
	Group        string
	ProblemCount int
}

type ProblemSummary struct {
	Ref        ProblemRef
	Number     int
	Title      string
	Difficulty Difficulty
	Status     Status
	URL        string
}

type Example struct {
	Input  string
	Output string
}

type Compiler struct {
	Slug    string // "python_make" — never derived from Title
	Title   string // "Python"
	Version string // "3.12.3"
}

type Problem struct {
	Ref          ProblemRef
	Number       int
	Title        string
	Difficulty   Difficulty
	Statement    string
	InputFormat  string
	OutputFormat string
	Constraints  string
	Notes        string
	Sections     map[string]string // unrecognised <h2> sections, kept verbatim
	Examples     []Example
	Languages    []Compiler
	URL          string
	FetchedAt    time.Time
}

// TestResult is one judged test. Only "open" (sample) tests carry data; hidden
// tests are reported as a count only.
type TestResult struct {
	Number      int
	Verdict     string
	IsSample    bool
	TimeMillis  int
	MemoryBytes int64
	Input       string // fetched from a presigned link, never the link itself
	Output      string // what the submission actually printed
	Answer      string // what was expected
}

// Submission is one judged attempt.
//
// Verdict is deliberately a plain string, not an enum: only WRONG_ANSWER has
// ever been observed, and treating an unrecognised verdict as success would be
// the worst failure this system could have. Use IsAccepted, never a switch.
type Submission struct {
	GlobalID          string
	Ref               ProblemRef
	CompilerSlug      string
	Status            string // PENDING -> FINISHED
	Verdict           string
	MaxTimeMillis     int
	MaxMemoryBytes    int64
	FirstFailedTest   int
	CompileLog        string
	TimeLimitMillis   int
	MemoryLimitBytes  int64
	OpenTests         []TestResult
	HiddenTestCount   int
	SubmittedAt       time.Time
}

// IsAccepted reports success. Verdicts are an open set, so anything not
// explicitly known to mean success counts as failure.
func IsAccepted(verdict string) bool {
	return verdict == "OK" || verdict == "ACCEPTED"
}

// IsFinished reports whether judging has completed.
func IsFinished(status string) bool { return status == "FINISHED" }
```

- [ ] **Step 4: Write the parser**

Create `internal/coderun/extract/status.go`:

```go
// Package extract turns CodeRun HTML into domain types.
//
// Every function here is pure: it takes strings and returns domain values.
// No Playwright type may appear in this package — that is what allows the
// whole parsing layer to be tested offline against captured fixtures.
package extract

import (
	"regexp"
	"strings"

	"coderun-agent/internal/coderun"
)

// CSS Modules hash every class name, so the only stable part of the status
// icon's class attribute is the "_type_<token>__" fragment. The localized
// aria-label ("Не решалась") is deliberately not used: it would break the
// moment the site is translated.
var statusTokenRe = regexp.MustCompile(`ProblemStatus_type_([a-z_]+)__`)

func ParseStatus(class string) coderun.Status {
	m := statusTokenRe.FindStringSubmatch(class)
	if m == nil {
		return coderun.StatusUnknown
	}
	switch m[1] {
	case "not_solved":
		return coderun.StatusNotSolved
	case "wrong":
		return coderun.StatusWrong
	case "solved":
		return coderun.StatusSolved
	default:
		return coderun.StatusUnknown
	}
}

// difficultyLabels maps observed and plausible Russian labels. Only "Средняя"
// has actually been seen; the others are best-effort and an unmatched label
// yields DifficultyUnknown with the raw text preserved.
var difficultyLabels = map[string]coderun.DifficultyLevel{
	"лёгкая":   coderun.DifficultyEasy,
	"легкая":   coderun.DifficultyEasy,
	"простая":  coderun.DifficultyEasy,
	"средняя":  coderun.DifficultyMedium,
	"сложная":  coderun.DifficultyHard,
	"трудная":  coderun.DifficultyHard,
}

func ParseDifficulty(label string) coderun.Difficulty {
	raw := strings.TrimSpace(label)
	level, ok := difficultyLabels[strings.ToLower(raw)]
	if !ok {
		level = coderun.DifficultyUnknown
	}
	return coderun.Difficulty{Level: level, Raw: raw}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -run 'Status|Difficulty' -v`
Expected: PASS, 7 subtests

- [ ] **Step 6: Commit**

```bash
git add internal/coderun/models.go internal/coderun/extract/status.go internal/coderun/extract/status_test.go
git commit -m "feat(extract): parse solve status from class token and difficulty label"
```

---

## Task 4: KaTeX normalisation

CodeRun renders formulas with KaTeX, which emits **both** an accessible MathML tree and a visually-styled HTML tree. Taking text from the container therefore duplicates every formula: `1 ≤ N ≤ 1 0 5 1≤N≤10 5`. Clean TeX lives in an `<annotation encoding="application/x-tex">` node inside the MathML branch.

This is the highest-value parser in the milestone: silently doubled constraints would corrupt every LLM prompt built on top of it later.

**Files:**
- Create: `internal/coderun/extract/katex.go`
- Test: `internal/coderun/extract/katex_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `extract.NormalizeKatex(sel *goquery.Selection)` — mutates the selection in place, replacing every `.katex` subtree with `$tex$` text. Called by `ParseProblem` before any text is read.

- [ ] **Step 1: Add goquery**

```bash
go get github.com/PuerkitoBio/goquery
```

- [ ] **Step 2: Write the failing test**

Create `internal/coderun/extract/katex_test.go`:

```go
package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestNormalizeKatexReplacesFormulaWithTex(t *testing.T) {
	// Structure mirrors real KaTeX output: a mathml branch carrying the
	// annotation, and an html branch carrying the visible glyphs.
	html := `<div id="root"><p>bound <span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">
       1 \le N \le 10^5
      </annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">1 ≤ N ≤ 1 0 5</span>` +
		`</span> ok</p></div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Find("#root")
	NormalizeKatex(root)

	got := strings.Join(strings.Fields(root.Text()), " ")
	want := "bound $1 \\le N \\le 10^5$ ok"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestNormalizeKatexRemovesDuplication(t *testing.T) {
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">N</annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">N</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	// Before normalisation this reads "N\nN". Exactly one N must survive.
	if got := strings.TrimSpace(root.Text()); got != "$N$" {
		t.Errorf("got %q, want %q", got, "$N$")
	}
}

func TestNormalizeKatexWithoutAnnotationDropsHiddenBranch(t *testing.T) {
	// Defensive: if KaTeX ever renders without an annotation, we must still
	// not emit the formula twice.
	//
	// The MathML branch carries real glyph text here (<mi>/<mo>), exactly as
	// KaTeX emits it. That matters: with an empty <semantics> a no-op
	// implementation would pass this test, making it useless as a guard.
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics><mrow>` +
		`<mi>x</mi><mo>+</mo><mi>y</mi></mrow></semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">x+y</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	if got := strings.TrimSpace(root.Text()); got != "x+y" {
		t.Errorf("got %q, want %q (a no-op implementation yields \"x+yx+y\")", got, "x+y")
	}
	if root.Find(".katex").Length() != 0 {
		t.Error("a .katex node survived the no-annotation path")
	}
}

func TestNormalizeKatexEscapesMarkupInTex(t *testing.T) {
	// Strict inequalities are everywhere in competitive programming. The TeX
	// source contains a literal '<', which must never be spliced into an HTML
	// string and re-parsed as a tag.
	//
	// The '<' must be followed immediately by a letter, with no space. HTML5
	// only enters tag-open state when '<' is directly followed by an ASCII
	// letter, so "0 < x" survives an unescaped splice by luck while "0<x"
	// does not. Only the no-space form discriminates a correct implementation
	// from a broken one.
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">0&lt;x&lt;10</annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">0&lt;x&lt;10</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	if got := strings.TrimSpace(root.Text()); got != "$0<x<10$" {
		t.Errorf("got %q, want %q — TeX was re-parsed as markup", got, "$0<x<10$")
	}
}

func TestNormalizeKatexOnRealFixture(t *testing.T) {
	html := loadFixture(t, "problem-bridge-to-the-palace.html")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	body := doc.Find("body")
	if body.Find(".katex").Length() == 0 {
		t.Skip("fixture contains no KaTeX; re-capture if the problem changed")
	}
	NormalizeKatex(body)

	if body.Find(".katex").Length() != 0 {
		t.Error("NormalizeKatex left .katex nodes behind")
	}
	// The doubled rendering of this problem's bound is "1 0 5 1≤N≤10 5".
	// Its disappearance is the point of this function.
	if strings.Contains(body.Text(), "1≤N≤10") {
		t.Error("doubled math survived normalisation")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/coderun/extract/ -run Katex -v`
Expected: FAIL — `undefined: NormalizeKatex`

- [ ] **Step 4: Write the implementation**

Create `internal/coderun/extract/katex.go`:

```go
package extract

import (
	"html"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// NormalizeKatex replaces every KaTeX formula inside sel with its TeX source
// wrapped in dollar signs, mutating the document in place.
//
// KaTeX renders each formula twice — once as MathML for screen readers and
// once as styled HTML for sighted users — so reading text without this step
// duplicates every formula. The TeX source is recovered from the
// <annotation encoding="application/x-tex"> node in the MathML branch.
//
// Recovered text is HTML-escaped before being spliced back in. goquery's
// Text() returns decoded text, and ReplaceWithHtml re-parses its argument as
// markup, so an unescaped strict inequality like "0 < x < 10" would be read
// as an opening tag and silently destroy the constraint.
func NormalizeKatex(sel *goquery.Selection) {
	sel.Find(".katex").Each(func(_ int, k *goquery.Selection) {
		tex := strings.TrimSpace(k.Find(`annotation[encoding="application/x-tex"]`).First().Text())

		if tex == "" {
			// No annotation to recover. Drop the MathML branch and unwrap the
			// node, so the visible rendering survives exactly once and no
			// .katex element is left behind.
			k.Find(".katex-mathml").Remove()
			visible := strings.TrimSpace(k.Text())
			k.ReplaceWithHtml("<span>" + html.EscapeString(visible) + "</span>")
			return
		}
		// Collapse internal whitespace: annotations arrive pretty-printed.
		// TeX is whitespace-insensitive in maths mode, so this is safe.
		tex = strings.Join(strings.Fields(tex), " ")
		k.ReplaceWithHtml("<span>$" + html.EscapeString(tex) + "$</span>")
	})
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -run Katex -v`
Expected: PASS, 5 tests

Before committing, confirm `TestNormalizeKatexEscapesMarkupInTex` is a real guard:
temporarily drop the `html.EscapeString` call and re-run it. It must FAIL. Restore
the call afterwards. A test that passes against the broken implementation is not
coverage.

- [ ] **Step 6: Commit**

```bash
git add internal/coderun/extract/katex.go internal/coderun/extract/katex_test.go
git commit -m "feat(extract): normalise KaTeX formulas to TeX and remove duplication"
```

---

## Task 5: The `?filters=` pagination blob

Problem lists are paginated, and page state travels in a query parameter holding **double-URL-encoded** JSON. This is an undocumented UI internal and a known fragile point, so it gets its own tested helper rather than being inlined at a call site.

Field order matters: the encoder must reproduce the site's exact byte layout, and Go marshals struct fields in declaration order.

**Files:**
- Create: `internal/coderun/extract/filters.go`
- Test: `internal/coderun/extract/filters_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `extract.Filters` struct with `CurrentPage` and `PageSize` fields
  - `extract.DefaultFilters(page int) Filters`
  - `extract.EncodeFilters(f Filters) (string, error)` — returns the double-escaped value to place after `filters=`
  - `extract.DecodeFilters(v string) (Filters, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/extract/filters_test.go`:

```go
package extract

import "testing"

// Captured verbatim from a real problem link on /selections/2025-summer-common.
const observedFilters = "%257B%2522difficulty%2522%253A%255B%255D%252C%2522search%2522%253A%2522%2522%252C" +
	"%2522sort%2522%253Anull%252C%2522status%2522%253A%255B%255D%252C%2522currentPage%2522%253A1%252C" +
	"%2522pageSize%2522%253A20%252C%2522tag%2522%253A%255B%255D%252C%2522language%2522%253A%255B%255D%252C" +
	"%2522groups%2522%253A%255B%255D%257D"

func TestEncodeFiltersMatchesObserved(t *testing.T) {
	got, err := EncodeFilters(DefaultFilters(1))
	if err != nil {
		t.Fatal(err)
	}
	if got != observedFilters {
		t.Errorf("encoding drifted from the observed format\ngot  %s\nwant %s", got, observedFilters)
	}
}

func TestDecodeObservedFilters(t *testing.T) {
	f, err := DecodeFilters(observedFilters)
	if err != nil {
		t.Fatal(err)
	}
	if f.CurrentPage != 1 {
		t.Errorf("CurrentPage = %d, want 1", f.CurrentPage)
	}
	if f.PageSize != 20 {
		t.Errorf("PageSize = %d, want 20", f.PageSize)
	}
}

func TestFiltersRoundTrip(t *testing.T) {
	in := DefaultFilters(3)
	enc, err := EncodeFilters(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeFilters(enc)
	if err != nil {
		t.Fatal(err)
	}
	if out.CurrentPage != 3 || out.PageSize != 20 {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestDecodeFiltersRejectsGarbage(t *testing.T) {
	if _, err := DecodeFilters("not-encoded-json"); err == nil {
		t.Fatal("expected an error decoding garbage")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/extract/ -run Filters -v`
Expected: FAIL — `undefined: EncodeFilters`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/extract/filters.go`:

```go
package extract

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Filters mirrors the JSON blob CodeRun stores in the ?filters= query
// parameter. Field order is load-bearing: it must match the site's own
// serialisation byte for byte, and encoding/json emits fields in declaration
// order.
//
// This is an undocumented UI internal and a known fragile point. If problem
// listing ever starts returning page 1 repeatedly, suspect this struct first.
type Filters struct {
	Difficulty  []string `json:"difficulty"`
	Search      string   `json:"search"`
	Sort        *string  `json:"sort"`
	Status      []string `json:"status"`
	CurrentPage int      `json:"currentPage"`
	PageSize    int      `json:"pageSize"`
	Tag         []string `json:"tag"`
	Language    []string `json:"language"`
	Groups      []string `json:"groups"`
}

// DefaultFilters returns the filter set the site itself uses for a given page.
// PageSize stays at the observed 20; raising an unvalidated parameter against
// an undocumented endpoint is exactly the sort of thing that gets an account
// flagged.
func DefaultFilters(page int) Filters {
	return Filters{
		Difficulty:  []string{},
		Search:      "",
		Sort:        nil,
		Status:      []string{},
		CurrentPage: page,
		PageSize:    20,
		Tag:         []string{},
		Language:    []string{},
		Groups:      []string{},
	}
}

// EncodeFilters produces the value that follows "filters=" in a URL. The site
// escapes the JSON twice, so "{" arrives as "%257B" rather than "%7B".
func EncodeFilters(f Filters) (string, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("marshal filters: %w", err)
	}
	return url.QueryEscape(url.QueryEscape(string(raw))), nil
}

func DecodeFilters(v string) (Filters, error) {
	var f Filters

	once, err := url.QueryUnescape(v)
	if err != nil {
		return f, fmt.Errorf("filters: first unescape: %w", err)
	}
	twice, err := url.QueryUnescape(once)
	if err != nil {
		return f, fmt.Errorf("filters: second unescape: %w", err)
	}
	if err := json.Unmarshal([]byte(twice), &f); err != nil {
		return f, fmt.Errorf("filters: unmarshal %q: %w", twice, err)
	}
	return f, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -run Filters -v`
Expected: PASS, 4 tests.

If `TestEncodeFiltersMatchesObserved` fails on escaping details, compare byte by byte — `url.QueryEscape` encodes a space as `+`, which is why `Search` is empty in the default. Do not "fix" the test to match the code; the observed string is ground truth.

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/extract/filters.go internal/coderun/extract/filters_test.go
git commit -m "feat(extract): encode and decode the double-escaped pagination filters blob"
```

---

## Task 6: Selection and problem-list parsing

**Files:**
- Create: `internal/coderun/extract/listing.go`
- Test: `internal/coderun/extract/listing_test.go`

**Interfaces:**
- Consumes: `ParseStatus`, `ParseDifficulty` (Task 3)
- Produces:
  - `extract.ParseSelections(html string) ([]coderun.Selection, error)`
  - `extract.ParseProblemList(html, selectionSlug string) ([]coderun.ProblemSummary, int, error)` — the int is the total page count, minimum 1

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/extract/listing_test.go`:

```go
package extract

import (
	"testing"

	"coderun-agent/internal/coderun"
)

func TestParseSelections(t *testing.T) {
	sels, err := ParseSelections(loadFixture(t, "selections.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sels) != 13 {
		t.Errorf("got %d selections, want 13 (the coderun-seasons group)", len(sels))
	}

	byslug := map[string]coderun.Selection{}
	for _, s := range sels {
		byslug[s.Slug] = s
	}

	// Slug and title are independent. This pair is the proof: a "2025" slug
	// carries a title with no year in it, and 2026 is the "Summer" one.
	if got := byslug["2025-summer-common"].Title; got != "CodeRun Boost Challenge" {
		t.Errorf("2025-summer-common title = %q, want %q", got, "CodeRun Boost Challenge")
	}
	if got := byslug["2026-summer-common"].Title; got != "CodeRun Summer Challenge" {
		t.Errorf("2026-summer-common title = %q, want %q", got, "CodeRun Summer Challenge")
	}
}

func TestParseSelectionsIgnoresNonSelectionLinks(t *testing.T) {
	sels, err := ParseSelections(loadFixture(t, "selections.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sels {
		if s.Slug == "" {
			t.Error("empty slug: a group/filter link leaked into the results")
		}
		// "?group=favourites" style links must not be treated as selections.
		if len(s.Slug) > 0 && s.Slug[0] == '?' {
			t.Errorf("query link leaked as a selection: %q", s.Slug)
		}
	}
}

func TestParseProblemList(t *testing.T) {
	probs, pages, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 20 {
		t.Errorf("got %d problems, want 20 on page 1", len(probs))
	}
	if pages != 2 {
		t.Errorf("got %d pages, want 2", pages)
	}

	first := probs[0]
	if first.Ref.ProblemSlug != "coderun-welcome" {
		t.Errorf("slug = %q, want coderun-welcome", first.Ref.ProblemSlug)
	}
	if first.Ref.SelectionSlug != "2025-summer-common" {
		t.Errorf("selection = %q", first.Ref.SelectionSlug)
	}
	if first.Number != 1 {
		t.Errorf("Number = %d, want 1", first.Number)
	}
	if first.Title != "Добро пожаловать в мир CodeRun!" {
		t.Errorf("Title = %q — the leading %q number prefix must be stripped", first.Title, "1. ")
	}
}

func TestParseProblemListReadsDifficulty(t *testing.T) {
	// Without this, a stub returning DifficultyUnknown for every row passes
	// the whole suite. Difficulty is derived by slicing row text after the
	// title, which is the most fragile extraction in this file.
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		if p.Difficulty.Raw == "" {
			t.Errorf("problem %q has an empty difficulty label — rowDifficulty is not finding it",
				p.Ref.ProblemSlug)
		}
	}
	if probs[0].Difficulty.Level == coderun.DifficultyUnknown {
		t.Errorf("problem %q difficulty %q did not map to a known level",
			probs[0].Ref.ProblemSlug, probs[0].Difficulty.Raw)
	}
}

func TestParseProblemListHandlesNonBreakingSpaces(t *testing.T) {
	// "В двоичном лесу" carries a U+00A0 after the single-letter preposition,
	// which is ordinary Russian typography rather than an edge case. Go's
	// regexp \s does not match U+00A0 while strings.Fields does, so an
	// un-normalised pipeline yields a title that cannot be found in its own
	// row text — and the difficulty silently disappears.
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	var found *coderun.ProblemSummary
	for i := range probs {
		if probs[i].Ref.ProblemSlug == "binary-forest" {
			found = &probs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("binary-forest not found; re-capture the fixture if the selection changed")
	}
	if found.Title != normalizeSpace(found.Title) {
		t.Errorf("Title %q is not whitespace-normalised (likely a stray U+00A0)", found.Title)
	}
	if found.Difficulty.Raw == "" {
		t.Error("difficulty was lost for a title containing a non-breaking space")
	}
}

func TestCountPagesWithoutPagerIsOne(t *testing.T) {
	// A selection short enough to fit on one page has no pager at all. That
	// must read as exactly one page, not zero.
	_, pages, err := ParseProblemList(
		`<div data-testid="problem-list-item">`+
			`<span role="graphics-symbol" class="ProblemStatus_type_solved__x"></span>`+
			`<a href="/selections/s/problems/only-one">1. Единственная Средняя</a></div>`, "s")
	if err != nil {
		t.Fatal(err)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want 1 when no pager is present", pages)
	}
}

func TestParseProblemListStripsFiltersFromSlug(t *testing.T) {
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		for _, c := range p.Ref.ProblemSlug {
			if c == '?' || c == '&' {
				t.Fatalf("slug %q still carries query parameters", p.Ref.ProblemSlug)
			}
		}
	}
}

func TestParseProblemListReadsStatus(t *testing.T) {
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		if p.Status == coderun.StatusUnknown {
			t.Errorf("problem %q has StatusUnknown — the status icon selector is wrong",
				p.Ref.ProblemSlug)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/extract/ -run 'Selections|ProblemList' -v`
Expected: FAIL — `undefined: ParseSelections`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/extract/listing.go`:

```go
package extract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"coderun-agent/internal/coderun"
)

func parse(html string) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	return doc, nil
}

// selectionHref matches "/selections/<slug>" and nothing deeper, so problem
// links and "?group=" filter links are both excluded.
var selectionHref = regexp.MustCompile(`^/selections/([a-z0-9-]+)$`)

func ParseSelections(html string) ([]coderun.Selection, error) {
	doc, err := parse(html)
	if err != nil {
		return nil, err
	}

	var out []coderun.Selection
	seen := map[string]bool{}

	doc.Find(`a[href^="/selections/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		m := selectionHref.FindStringSubmatch(href)
		if m == nil {
			return
		}
		slug := m[1]
		if seen[slug] {
			return
		}
		title := strings.TrimSpace(a.Text())
		if title == "" {
			return
		}
		seen[slug] = true
		out = append(out, coderun.Selection{Slug: slug, Title: title})
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("no selections found: the page structure has changed")
	}
	return out, nil
}

// numberPrefix splits "12. Заголовок" into 12 and "Заголовок".
var numberPrefix = regexp.MustCompile(`^\s*(\d+)\.\s*(.+)$`)

// problemHref pulls the slug out of "/selections/<sel>/problems/<slug>?filters=…".
var problemHref = regexp.MustCompile(`^/selections/[^/]+/problems/([^/?#]+)`)

func ParseProblemList(html, selectionSlug string) ([]coderun.ProblemSummary, int, error) {
	doc, err := parse(html)
	if err != nil {
		return nil, 0, err
	}

	var out []coderun.ProblemSummary

	doc.Find(`[data-testid="problem-list-item"]`).Each(func(_ int, row *goquery.Selection) {
		a := row.Find(`a[href*="/problems/"]`).First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		m := problemHref.FindStringSubmatch(href)
		if m == nil {
			return
		}
		slug := m[1]

		// Normalise before matching. Russian typography puts non-breaking
		// spaces (U+00A0) after single-letter prepositions — "В двоичном
		// лесу" — and Go's regexp \s is ASCII-only while strings.Fields and
		// strings.TrimSpace are Unicode-aware. Mixing the two silently
		// produces titles that no longer match the text they came from.
		number, title := 0, normalizeSpace(a.Text())
		if nm := numberPrefix.FindStringSubmatch(title); nm != nil {
			number, _ = strconv.Atoi(nm[1])
			title = strings.TrimSpace(nm[2])
		}

		class, _ := row.Find(`span[role="graphics-symbol"]`).First().Attr("class")

		out = append(out, coderun.ProblemSummary{
			Ref: coderun.ProblemRef{
				SelectionSlug: selectionSlug,
				ProblemSlug:   slug,
			},
			Number:     number,
			Title:      title,
			Difficulty: ParseDifficulty(rowDifficulty(row, title)),
			Status:     ParseStatus(class),
			URL:        fmt.Sprintf("/selections/%s/problems/%s", selectionSlug, slug),
		})
	})

	if len(out) == 0 {
		return nil, 0, fmt.Errorf("no problems found: the problem-list-item selector has changed")
	}
	return out, countPages(doc), nil
}

// normalizeSpace collapses every run of Unicode whitespace — including the
// non-breaking spaces CodeRun's Russian titles are full of — to a single
// ASCII space. Both sides of any text comparison in this file must go through
// it, or a title containing U+00A0 will fail to match the row text it was
// extracted from.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// rowDifficulty recovers the difficulty label, which sits in the row's text
// after the title and has no dedicated test id.
func rowDifficulty(row *goquery.Selection, title string) string {
	needle := normalizeSpace(title)
	if needle == "" {
		return ""
	}
	text := normalizeSpace(row.Text())
	if i := strings.LastIndex(text, needle); i >= 0 {
		return strings.TrimSpace(text[i+len(needle):])
	}
	return ""
}

// pageLinkLabel matches the pager's per-page control, whose accessible name is
// "К странице <n>". Matching the ARIA label rather than "any number inside any
// <nav>" avoids picking up breadcrumbs or unrelated navigation — the page
// carries both a Breadcrumbs nav and a Pagination nav.
//
// This is locale-dependent, which is a real weakness. It is accepted because
// the alternative is a structural guess that fails silently: an
// under-counted pager means whole pages of problems are never crawled and no
// error is raised. ListProblems carries a second guard for that case.
var pageLinkLabel = regexp.MustCompile(`^К странице (\d+)$`)

// countPages reads the highest page number offered by the pager. A single-page
// list has no pager, which correctly yields 1.
func countPages(doc *goquery.Document) int {
	max := 1
	doc.Find(`[aria-label]`).Each(func(_ int, e *goquery.Selection) {
		label, _ := e.Attr("aria-label")
		m := pageLinkLabel.FindStringSubmatch(strings.TrimSpace(label))
		if m == nil {
			return
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	})
	return max
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -run 'Selections|ProblemList' -v`
Expected: PASS, 5 tests.

If `TestParseProblemListReadsStatus` fails, inspect the fixture for the real status-icon markup rather than loosening the assertion — an unknown status silently means "never attempted" downstream, which is a real bug.

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/extract/listing.go internal/coderun/extract/listing_test.go
git commit -m "feat(extract): parse selection and paginated problem listings"
```

---

## Task 7: Problem statement extraction

**Files:**
- Create: `internal/coderun/extract/statement.go`
- Test: `internal/coderun/extract/statement_test.go`

**Interfaces:**
- Consumes: `NormalizeKatex` (Task 4), `ParseDifficulty` (Task 3)
- Produces: `extract.ParseProblem(html string, ref coderun.ProblemRef) (*coderun.Problem, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/extract/statement_test.go`:

```go
package extract

import (
	"strings"
	"testing"

	"coderun-agent/internal/coderun"
)

func loadProblem(t *testing.T) *coderun.Problem {
	t.Helper()
	ref := coderun.ProblemRef{
		SelectionSlug: "2025-summer-common",
		ProblemSlug:   "bridge-to-the-palace",
	}
	p, err := ParseProblem(loadFixture(t, "problem-bridge-to-the-palace.html"), ref)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseProblemHeader(t *testing.T) {
	p := loadProblem(t)

	if p.Title != "Мост во дворец" {
		t.Errorf("Title = %q, want %q (number prefix stripped)", p.Title, "Мост во дворец")
	}
	if p.Number != 2 {
		t.Errorf("Number = %d, want 2", p.Number)
	}
	if p.Difficulty.Raw != "Средняя" {
		t.Errorf("Difficulty.Raw = %q, want Средняя", p.Difficulty.Raw)
	}
}

func TestParseProblemSections(t *testing.T) {
	p := loadProblem(t)

	if p.Statement == "" {
		t.Error("Statement is empty")
	}
	if p.InputFormat == "" {
		t.Error("InputFormat is empty — the Формат ввода heading was not matched")
	}
	if p.OutputFormat == "" {
		t.Error("OutputFormat is empty — the Формат вывода heading was not matched")
	}
	// The statement body must stop before the first h2.
	if strings.Contains(p.Statement, "Формат ввода") {
		t.Error("Statement bled past the first h2 boundary")
	}
}

func TestParseProblemMathIsNotDoubled(t *testing.T) {
	p := loadProblem(t)

	all := p.Statement + p.InputFormat + p.OutputFormat + p.Constraints
	if strings.Contains(all, "1≤N≤10") {
		t.Error("doubled KaTeX rendering survived into the parsed problem")
	}
	if !strings.Contains(all, "$") {
		t.Error("no TeX markers found — NormalizeKatex was not applied before reading text")
	}
}

func TestParseProblemExamples(t *testing.T) {
	p := loadProblem(t)

	if len(p.Examples) != 2 {
		t.Fatalf("got %d examples, want 2", len(p.Examples))
	}
	if got := strings.TrimSpace(p.Examples[0].Input); got != "5\n2 0 -3 3 6" {
		t.Errorf("Examples[0].Input = %q", got)
	}
	if got := strings.TrimSpace(p.Examples[0].Output); got != "2" {
		t.Errorf("Examples[0].Output = %q", got)
	}
	if got := strings.TrimSpace(p.Examples[1].Output); got != "0" {
		t.Errorf("Examples[1].Output = %q", got)
	}
	// The "Ввод"/"Вывод" label line must be stripped, not kept.
	if strings.HasPrefix(strings.TrimSpace(p.Examples[0].Input), "Ввод") {
		t.Error("example label line was not stripped")
	}
}

func TestParseProblemKeepsUnknownSections(t *testing.T) {
	p := loadProblem(t)

	// This problem has a Примечание section, which is mapped. The point of the
	// assertion is that Sections exists and unmapped headings would land there
	// rather than being dropped.
	if p.Sections == nil {
		t.Error("Sections map must be non-nil so unknown headings are never lost")
	}
	if p.Notes == "" {
		t.Error("Notes is empty — the Примечание heading was not matched")
	}
}

func TestParseProblemDoesNotDuplicateExamplesIntoSections(t *testing.T) {
	// The examples block has its own <h2>Примеры</h2>. Left unignored, that
	// heading falls into the unrecognised-section branch and the whole block
	// is collected as run-together text, duplicating Problem.Examples inside
	// a field meant for genuine unrecognised prose.
	p := loadProblem(t)

	if _, ok := p.Sections["примеры"]; ok {
		t.Error("Sections contains the examples block; it belongs only in Problem.Examples")
	}
	for key, body := range p.Sections {
		if strings.Contains(body, "Ввод") && strings.Contains(body, "Вывод") {
			t.Errorf("Sections[%q] carries example data: %.80q", key, body)
		}
	}
}

func TestParseProblemCompilers(t *testing.T) {
	p := loadProblem(t)

	if len(p.Languages) < 10 {
		t.Fatalf("got %d compilers, want at least 10", len(p.Languages))
	}
	found := map[string]coderun.Compiler{}
	for _, c := range p.Languages {
		found[c.Slug] = c
	}
	// JavaScript's slug is nodejs_20_make. This is the canonical proof that
	// slugs cannot be derived from language names.
	js, ok := found["nodejs_20_make"]
	if !ok {
		t.Fatal("nodejs_20_make missing: compiler slugs must be scraped, never derived")
	}
	if !strings.Contains(js.Title, "JavaScript") {
		t.Errorf("nodejs_20_make title = %q", js.Title)
	}
	if _, ok := found["python_make"]; !ok {
		t.Error("python_make missing")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/extract/ -run ParseProblem -v`
Expected: FAIL — `undefined: ParseProblem`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/extract/statement.go`:

```go
package extract

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"coderun-agent/internal/coderun"
)

// sectionHeadings maps the Russian <h2> labels observed on the site to fields.
// Anything not in this table is preserved in Problem.Sections rather than
// dropped: the heading set is an assumption drawn from one observed problem.
var sectionHeadings = map[string]string{
	"формат ввода":  "input",
	"формат вывода": "output",
	"ограничения":   "constraints",
	"примечание":    "notes",
}

// ignoredHeadings name sections whose content is captured structurally
// elsewhere. The examples block sits under its own <h2>Примеры</h2>, and its
// body is already parsed into Problem.Examples from the code-snippet blocks.
// Without this, that heading falls through to the unrecognised-section branch
// and the whole examples block is slurped into Sections as run-together text
// ("Пример 1Ввод5\n2 0 -3 3 6\nВывод2…") — a garbled duplicate inside a field
// documented as holding genuine unrecognised prose.
var ignoredHeadings = map[string]bool{
	"примеры": true,
}

// ignoredSection is the sentinel section key whose buffered body is discarded.
const ignoredSection = "\x00ignored"

func ParseProblem(html string, ref coderun.ProblemRef) (*coderun.Problem, error) {
	doc, err := parse(html)
	if err != nil {
		return nil, err
	}

	title := doc.Find(`[data-testid="problem-title"]`).First()
	if title.Length() == 0 {
		return nil, fmt.Errorf("problem-title not found: the page structure has changed")
	}

	// The description container is the title's nearest ancestor that also holds
	// the section headings. Selecting by its hashed class would be fragile.
	container := title.Parent()
	for container.Length() > 0 && container.Find("h2").Length() == 0 {
		container = container.Parent()
	}
	if container.Length() == 0 {
		return nil, fmt.Errorf("no statement container with h2 sections found")
	}

	// Must run before any text is read, or every formula appears twice.
	NormalizeKatex(container)

	p := &coderun.Problem{
		Ref:       ref,
		Sections:  map[string]string{},
		URL:       fmt.Sprintf("/selections/%s/problems/%s", ref.SelectionSlug, ref.ProblemSlug),
		FetchedAt: time.Now().UTC(),
	}

	headingText := strings.TrimSpace(title.Text())
	p.Number, p.Title = splitNumberPrefix(headingText)
	p.Difficulty = ParseDifficulty(findDifficulty(container))
	p.Examples = parseExamples(doc)
	p.Languages = parseCompilers(doc)

	assignSections(container, p)
	return p, nil
}

func splitNumberPrefix(s string) (int, string) {
	if m := numberPrefix.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, strings.TrimSpace(m[2])
	}
	return 0, s
}

// findDifficulty looks for a known difficulty label among the container's short
// text nodes. The label has no test id of its own.
func findDifficulty(container *goquery.Selection) string {
	var found string
	container.Find("span, div, p").EachWithBreak(func(_ int, e *goquery.Selection) bool {
		txt := strings.TrimSpace(e.Text())
		if len(txt) > 20 {
			return true
		}
		if _, ok := difficultyLabels[strings.ToLower(txt)]; ok {
			found = txt
			return false
		}
		return true
	})
	return found
}

// assignSections walks the container's h2 boundaries, collecting the text that
// follows each heading until the next one. Content before the first h2 is the
// statement body.
func assignSections(container *goquery.Selection, p *coderun.Problem) {
	var current string
	var buf []string

	flush := func() {
		text := clean(strings.Join(buf, "\n"))
		buf = buf[:0]
		if text == "" || current == ignoredSection {
			return
		}
		switch current {
		case "":
			p.Statement = text
		case "input":
			p.InputFormat = text
		case "output":
			p.OutputFormat = text
		case "constraints":
			p.Constraints = text
		case "notes":
			p.Notes = text
		default:
			p.Sections[current] = text
		}
	}

	container.Children().Each(func(_ int, node *goquery.Selection) {
		walkForSections(node, &current, &buf, flush)
	})
	flush()
}

func walkForSections(node *goquery.Selection, current *string, buf *[]string, flush func()) {
	if goquery.NodeName(node) == "h2" {
		flush()
		label := strings.ToLower(strings.TrimSpace(node.Text()))
		switch {
		case ignoredHeadings[label]:
			*current = ignoredSection
		default:
			if key, ok := sectionHeadings[label]; ok {
				*current = key
			} else {
				*current = label // preserved verbatim in Sections
			}
		}
		return
	}
	if node.Find("h2").Length() > 0 {
		node.Children().Each(func(_ int, child *goquery.Selection) {
			walkForSections(child, current, buf, flush)
		})
		return
	}
	if txt := clean(node.Text()); txt != "" {
		*buf = append(*buf, txt)
	}
}

// parseExamples reads the code-snippet blocks, which alternate Ввод / Вывод.
//
// Each snippet is a header element carrying the caption plus a <pre> holding
// the data. Read the <pre> directly: taking the whole snippet's text runs the
// caption straight into the content with no separator ("Ввод5\n2 0 -3 3 6"),
// so there is no newline for a label-stripper to find.
func parseExamples(doc *goquery.Document) []coderun.Example {
	var blocks []string
	doc.Find(`[data-testid="code-snippet"]`).Each(func(_ int, s *goquery.Selection) {
		if pre := s.Find("pre").First(); pre.Length() > 0 {
			blocks = append(blocks, strings.Trim(pre.Text(), "\n"))
			return
		}
		// Fallback for a snippet rendered without a <pre>.
		blocks = append(blocks, stripSnippetLabel(s.Text()))
	})

	var out []coderun.Example
	for i := 0; i+1 < len(blocks); i += 2 {
		out = append(out, coderun.Example{Input: blocks[i], Output: blocks[i+1]})
	}
	return out
}

// stripSnippetLabel removes the leading "Ввод" / "Вывод" caption line.
func stripSnippetLabel(s string) string {
	s = strings.TrimLeft(s, "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		first := strings.TrimSpace(s[:i])
		if first == "Ввод" || first == "Вывод" {
			return strings.TrimRight(s[i+1:], "\n")
		}
	}
	return strings.TrimRight(s, "\n")
}

// parseCompilers reads the language picker, which is a native <select> in the
// server-rendered page. The option's value attribute IS the compilerSlug — it
// is never derived from the visible label, because JavaScript's slug is
// nodejs_20_make.
//
// A hidden placeholder option with an empty value is present and skipped.
//
// Version is left empty here. The static <select> carries only the language
// name ("JavaScript"); the versioned label ("JavaScript 20.14.0") appears only
// in the rich dropdown the client renders once opened, which is not worth a
// browser interaction for a cosmetic field.
func parseCompilers(doc *goquery.Document) []coderun.Compiler {
	var out []coderun.Compiler
	doc.Find(`select option`).Each(func(_ int, o *goquery.Selection) {
		slug, ok := o.Attr("value")
		if !ok || slug == "" {
			return
		}
		label := normalizeSpace(o.Text())
		title, version := label, ""
		// Split a trailing version if the label happens to carry one.
		if i := strings.LastIndex(label, " "); i > 0 {
			candidate := label[i+1:]
			if len(candidate) > 0 && candidate[0] >= '0' && candidate[0] <= '9' {
				title, version = label[:i], candidate
			}
		}
		out = append(out, coderun.Compiler{Slug: slug, Title: title, Version: version})
	})
	return out
}

func clean(s string) string {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			lines = append(lines, t)
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/extract/ -run ParseProblem -v`
Expected: PASS, 6 tests.

Two things this task's tests pinned down against the real page, already reflected in the code above:

- Compilers come from a native `<select>`, not an ARIA listbox. `[role="option"]` appears **zero** times in the captured page — the rich listbox is built client-side only after the dropdown is opened. The `<select>` is present server-side with `value="python_make"` style options, which is strictly better: no interaction needed.
- Snippet captions are a sibling element, not a first line. The snippet's own text reads `"Ввод5\n2 0 -3 3 6"` with no separator between caption and content, so reading the inner `<pre>` is the only reliable route.

- [ ] **Step 5: Run the whole extract suite**

Run: `go test ./internal/coderun/extract/ -v`
Expected: PASS, all tests, no network access.

- [ ] **Step 6: Commit**

```bash
git add internal/coderun/extract/statement.go internal/coderun/extract/statement_test.go
git commit -m "feat(extract): parse problem statement, sections, examples and compilers"
```

---

## Task 8: SQLite storage

SQLite is the single source of truth. The rule that keeps it that way: **if the program reads it, it is in the database.** Files under `solutions/` (Task 9) are written for humans and never read back.

**Files:**
- Create: `internal/storage/sqlite.go`
- Test: `internal/storage/sqlite_test.go`

**Interfaces:**
- Consumes: `coderun` domain types (Task 3)
- Produces:
  - `storage.Open(path string) (*Store, error)`, `(*Store).Close() error`
  - `(*Store).UpsertSelections(ctx, []coderun.Selection) error`
  - `(*Store).ListSelections(ctx) ([]coderun.Selection, error)`
  - `(*Store).UpsertProblems(ctx, []coderun.ProblemSummary) error`
  - `(*Store).SetContextID(ctx, ref coderun.ProblemRef) error`
  - `(*Store).GetContextID(ctx, selectionSlug, problemSlug string) (int, error)`
  - `(*Store).SaveProblem(ctx, *coderun.Problem) error`
  - `(*Store).NextAttempt(ctx, selectionSlug, problemSlug string) (int, error)`
  - `(*Store).Counts(ctx) (map[coderun.Status]int, error)`

- [ ] **Step 1: Add the driver**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Write the failing test**

Create `internal/storage/sqlite_test.go`:

```go
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
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/storage/ -v`
Expected: FAIL — `undefined: Open`

- [ ] **Step 4: Write the implementation**

Create `internal/storage/sqlite.go`:

```go
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
```

Add `internal/storage/marshal.go`:

```go
package storage

import (
	"encoding/json"

	"coderun-agent/internal/coderun"
)

func marshalProblem(p *coderun.Problem) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalProblem is used by the export command.
func UnmarshalProblem(blob []byte) (*coderun.Problem, error) {
	var p coderun.Problem
	if err := json.Unmarshal(blob, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/storage/ -v`
Expected: PASS, 6 tests

- [ ] **Step 6: Commit**

```bash
git add internal/storage/
git commit -m "feat(storage): add SQLite schema, upserts and attempt numbering"
```

---

## Task 9: Solution artifacts

Write-only files for humans, per PLAN.md §15. Attempts are never overwritten.

**Files:**
- Create: `internal/storage/artifacts.go`
- Test: `internal/storage/artifacts_test.go`

**Interfaces:**
- Consumes: `coderun.ProblemRef` (Task 3)
- Produces:
  - `storage.AttemptMeta` struct
  - `storage.WriteAttempt(root string, ref coderun.ProblemRef, attempt int, ext string, source []byte, meta AttemptMeta) error`

- [ ] **Step 1: Write the failing test**

Create `internal/storage/artifacts_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/storage/ -run Attempt -v`
Expected: FAIL — `undefined: WriteAttempt`

- [ ] **Step 3: Write the implementation**

Create `internal/storage/artifacts.go`:

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/storage/ -v`
Expected: PASS, 9 tests

- [ ] **Step 5: Commit**

```bash
git add internal/storage/artifacts.go internal/storage/artifacts_test.go
git commit -m "feat(storage): write immutable per-attempt solution artifacts"
```

---

## Task 10: Browser lifecycle and challenge detection

**Files:**
- Create: `internal/coderun/playwright/browser.go`
- Test: `internal/coderun/playwright/browser_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.BaseURL` (Task 1)
- Produces:
  - `pwclient.Browser` struct with `Ctx playwright.BrowserContext` and `Page playwright.Page`
  - `pwclient.New(cfg *config.Config, headedOverride bool) (*Browser, error)`
  - `(*Browser).Close() error`
  - `(*Browser).Goto(ctx context.Context, path string) (string, error)` — navigates, sleeps `RequestDelay`, runs challenge detection, returns page HTML
  - `pwclient.ErrChallenge` sentinel error
  - `pwclient.DetectChallenge(url, html string) error` — pure, unit-tested

- [ ] **Step 1: Confirm Playwright is available**

The dependency and the Chromium build were installed in Task 2. Verify rather than reinstall:

```bash
go list -m github.com/mxschmitt/playwright-go
```

If it is missing, run `go get github.com/mxschmitt/playwright-go` and
`go run github.com/mxschmitt/playwright-go/cmd/playwright@latest install chromium --with-deps`.
The `mxschmitt` path is correct — see the Playwright import path note under Tech Stack.

- [ ] **Step 2: Write the failing test**

Challenge detection is pure, so it is tested without a browser. Everything else in this file needs a live Chromium and belongs to Task 16.

Create `internal/coderun/playwright/browser_test.go`:

```go
package pwclient

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectChallengeOnPassportRedirect(t *testing.T) {
	// Being bounced to passport mid-run means the session died. That is a stop
	// condition, never something to retry around.
	err := DetectChallenge("https://passport.yandex.ru/auth?retpath=x", "<html></html>")
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeOnCaptchaHost(t *testing.T) {
	err := DetectChallenge("https://coderun.yandex.ru/showcaptcha?t=1", "<html></html>")
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeOnCaptchaMarkup(t *testing.T) {
	html := `<html><body><div class="CheckboxCaptcha">Подтвердите, что запросы отправляли вы</div></body></html>`
	err := DetectChallenge("https://coderun.yandex.ru/selections", html)
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeAllowsNormalPage(t *testing.T) {
	html := `<html><body><div data-testid="problem-list-item">1. Задача</div></body></html>`
	if err := DetectChallenge("https://coderun.yandex.ru/selections/2025-summer-common", html); err != nil {
		t.Errorf("normal page flagged as a challenge: %v", err)
	}
}

func TestChallengeErrorMentionsURL(t *testing.T) {
	err := DetectChallenge("https://passport.yandex.ru/auth", "")
	if err == nil || !strings.Contains(err.Error(), "passport.yandex.ru") {
		t.Errorf("error should name the URL that triggered it, got %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: FAIL — `undefined: DetectChallenge`

- [ ] **Step 4: Write the implementation**

Create `internal/coderun/playwright/browser.go`:

```go
// Package pwclient drives CodeRun through a real browser.
//
// One persistent context, one page, sequential navigation. The package never
// attempts to evade anti-bot measures: if the site challenges us, the run
// stops and reports.
package pwclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"coderun-agent/internal/config"
)

// ErrChallenge means the site asked for human verification, or the session
// expired. Callers must surface it, never retry through it.
var ErrChallenge = errors.New("coderun presented a challenge")

type Browser struct {
	pw        *playwright.Playwright
	Ctx       playwright.BrowserContext
	Page      playwright.Page
	delay     time.Duration
	maxSource int64
	maxArtifact int64
}

// New launches a persistent browser context. headedOverride forces a visible
// browser regardless of config, which `auth login` uses because logging in is
// inherently interactive.
func New(cfg *config.Config, headedOverride bool) (*Browser, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright (did you run `playwright install chromium`?): %w", err)
	}

	headless := cfg.Headless && !headedOverride

	ctx, err := pw.Chromium.LaunchPersistentContext(cfg.BrowserProfileDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(headless),
		})
	if err != nil {
		pw.Stop()
		return nil, fmt.Errorf("launch persistent context at %s: %w", cfg.BrowserProfileDir, err)
	}

	// A persistent context opens with one page already present.
	var page playwright.Page
	if pages := ctx.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = ctx.NewPage(); err != nil {
		ctx.Close()
		pw.Stop()
		return nil, fmt.Errorf("open page: %w", err)
	}

	return &Browser{
		pw:          pw,
		Ctx:         ctx,
		Page:        page,
		delay:       cfg.RequestDelay,
		maxSource:   cfg.MaxSourceBytes,
		maxArtifact: cfg.MaxArtifactBytes,
	}, nil
}

func (b *Browser) Close() error {
	var firstErr error
	if err := b.Ctx.Close(); err != nil {
		firstErr = err
	}
	if err := b.pw.Stop(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Goto navigates to a site-relative path and returns the page HTML. It applies
// the configured request delay before navigating and checks for challenges
// after, so every caller inherits both behaviours.
func (b *Browser) Goto(ctx context.Context, path string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(b.delay):
	}

	url := config.BaseURL + path
	slog.Debug("navigating", "path", path)

	if _, err := b.Page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return "", fmt.Errorf("navigate to %s: %w", path, err)
	}

	html, err := b.Page.Content()
	if err != nil {
		return "", fmt.Errorf("read page content: %w", err)
	}
	if err := DetectChallenge(b.Page.URL(), html); err != nil {
		return "", err
	}
	return html, nil
}

// challengeMarkers are substrings that indicate a verification page rather
// than content. Kept deliberately narrow to avoid false positives.
var challengeMarkers = []string{
	"CheckboxCaptcha",
	"SmartCaptcha",
	"Подтвердите, что запросы отправляли вы",
}

// DetectChallenge reports whether a loaded page is a verification wall or an
// unexpected logout. Pure, so it is unit-tested without a browser.
func DetectChallenge(url, html string) error {
	if strings.Contains(url, "passport.yandex.ru") {
		return fmt.Errorf("%w: redirected to %s — run `coderun-agent auth login`", ErrChallenge, "passport.yandex.ru")
	}
	if strings.Contains(url, "showcaptcha") {
		return fmt.Errorf("%w: captcha at %s — complete it manually in the browser", ErrChallenge, url)
	}
	for _, marker := range challengeMarkers {
		if strings.Contains(html, marker) {
			return fmt.Errorf("%w: verification page detected at %s", ErrChallenge, url)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: PASS, 5 tests, no browser launched

- [ ] **Step 6: Commit**

```bash
git add internal/coderun/playwright/browser.go internal/coderun/playwright/browser_test.go
git commit -m "feat(browser): add persistent context lifecycle and challenge detection"
```

---

## Task 11: Authentication

No password ever enters the program. `auth login` opens a visible browser and waits for the human to finish.

**Files:**
- Create: `internal/coderun/playwright/auth.go`
- Test: `internal/coderun/playwright/auth_test.go`

**Interfaces:**
- Consumes: `Browser` (Task 10)
- Produces:
  - `pwclient.IsLoggedIn(html string) bool` — pure
  - `(*Browser).AuthStatus(ctx context.Context) (bool, error)`
  - `(*Browser).AwaitLogin(ctx context.Context, timeout time.Duration) error`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/playwright/auth_test.go`:

```go
package pwclient

import "testing"

func TestIsLoggedInFalseWhenLoginControlPresent(t *testing.T) {
	html := `<html><body><a data-testid="log-in" href="https://passport.yandex.ru/auth">Войти</a></body></html>`
	if IsLoggedIn(html) {
		t.Error("a page carrying the log-in control must not count as authenticated")
	}
}

func TestIsLoggedInTrueWhenLoginControlAbsent(t *testing.T) {
	html := `<html><body><button aria-label="Меню профиля"></button><div data-testid="problem-title">2. X</div></body></html>`
	if !IsLoggedIn(html) {
		t.Error("a page without the log-in control should count as authenticated")
	}
}

func TestIsLoggedInFalseOnEmptyPage(t *testing.T) {
	// An empty or error page must never be read as a valid session.
	if IsLoggedIn("") {
		t.Error("empty HTML must not count as authenticated")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/playwright/ -run LoggedIn -v`
Expected: FAIL — `undefined: IsLoggedIn`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/playwright/auth.go`:

```go
package pwclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// loginMarker is the control CodeRun renders only for anonymous visitors.
const loginMarker = `data-testid="log-in"`

// IsLoggedIn reports whether a CodeRun page was rendered for an authenticated
// user. Absence of the log-in control is the signal; an empty page is never
// treated as logged in.
func IsLoggedIn(html string) bool {
	if strings.TrimSpace(html) == "" {
		return false
	}
	return !strings.Contains(html, loginMarker)
}

// AuthStatus loads a cheap CodeRun page and reports session validity.
func (b *Browser) AuthStatus(ctx context.Context) (bool, error) {
	html, err := b.Goto(ctx, "/selections?group=coderun-seasons")
	if err != nil {
		// A challenge here means "not authenticated", which is an answer
		// rather than a failure.
		if isChallenge(err) {
			return false, nil
		}
		return false, err
	}
	return IsLoggedIn(html), nil
}

func isChallenge(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrChallenge.Error())
}

// AwaitLogin opens the Yandex login page and waits for the operator to finish.
//
// The program never types credentials, never answers a CAPTCHA and never
// handles 2FA. It only watches for the session to become valid.
func (b *Browser) AwaitLogin(ctx context.Context, timeout time.Duration) error {
	const loginURL = "https://passport.yandex.ru/auth?retpath=https%3A%2F%2Fcoderun.yandex.ru%2Fselections"

	if _, err := b.Page.Goto(loginURL); err != nil {
		return fmt.Errorf("open login page: %w", err)
	}

	fmt.Println("A browser window is open. Please log into your Yandex account there.")
	fmt.Println("Complete any 2FA or CAPTCHA yourself — this program will not touch them.")
	fmt.Println("Waiting for the session to become active...")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}

		ok, err := b.AuthStatus(ctx)
		if err != nil {
			slog.Debug("waiting for login", "error", err)
			continue
		}
		if ok {
			fmt.Println("Authenticated. The session is stored in the browser profile.")
			return nil
		}
	}
	return fmt.Errorf("timed out after %s waiting for login", timeout)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: PASS, 8 tests

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/playwright/auth.go internal/coderun/playwright/auth_test.go
git commit -m "feat(auth): add manual browser login and session status checks"
```

---

## Task 12: Crawling — selections, problems, statements

Wires the browser to the pure parsers, adds pagination, and captures `ContextID` from the page's own `solution-template` request.

**Files:**
- Create: `internal/coderun/playwright/crawl.go`, `internal/coderun/client.go`
- Test: `internal/coderun/playwright/crawl_test.go`
- Imports: `crawl.go` needs `coderun-agent/internal/config` (for `config.BaseURL`), `coderun-agent/internal/coderun`, `coderun-agent/internal/coderun/extract`, `github.com/playwright-community/playwright-go`, plus `context`, `encoding/json`, `fmt`, `log/slog`, `net/url`, `strconv`, `sync`

**Interfaces:**
- Consumes: `Browser.Goto` (Task 10), all `extract` parsers (Tasks 3–7)
- Produces:
  - `coderun.CodeRunClient` interface
  - `(*Browser).ListSelections(ctx, group string) ([]coderun.Selection, error)`
  - `(*Browser).ListProblems(ctx, selectionSlug string) ([]coderun.ProblemSummary, error)`
  - `(*Browser).GetProblem(ctx, ref coderun.ProblemRef) (*coderun.Problem, int, error)` — the int is the discovered ContextID
  - `pwclient.ProblemPath(ref coderun.ProblemRef, compilerSlug string) string` — pure
  - `pwclient.ParseContextID(url string) (int, error)` — pure

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/playwright/crawl_test.go`:

```go
package pwclient

import (
	"testing"

	"coderun-agent/internal/coderun"
)

func TestProblemPath(t *testing.T) {
	ref := coderun.ProblemRef{SelectionSlug: "2025-summer-common", ProblemSlug: "bridge-to-the-palace"}

	if got, want := ProblemPath(ref, ""), "/selections/2025-summer-common/problems/bridge-to-the-palace"; got != want {
		t.Errorf("ProblemPath() = %q, want %q", got, want)
	}
	// Language is selected by URL parameter rather than by clicking the
	// dropdown: fewer interactions, and it is idempotent.
	want := "/selections/2025-summer-common/problems/bridge-to-the-palace?compiler=python_make"
	if got := ProblemPath(ref, "python_make"); got != want {
		t.Errorf("ProblemPath() = %q, want %q", got, want)
	}
}

func TestParseContextID(t *testing.T) {
	url := "https://coderun.yandex.ru/api/problem/bridge-to-the-palace/solution-template?compilerSlug=dart_make&problemContextId=1838"
	got, err := ParseContextID(url)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1838 {
		t.Errorf("ParseContextID() = %d, want 1838", got)
	}
}

func TestParseContextIDMissing(t *testing.T) {
	if _, err := ParseContextID("https://coderun.yandex.ru/api/problem/x/solution-template?compilerSlug=go_make"); err == nil {
		t.Fatal("expected an error when problemContextId is absent")
	}
}

func TestParseContextIDNotANumber(t *testing.T) {
	if _, err := ParseContextID("https://x/?problemContextId=abc"); err == nil {
		t.Fatal("expected an error for a non-numeric id")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/playwright/ -run 'ProblemPath|ContextID' -v`
Expected: FAIL — `undefined: ProblemPath`

- [ ] **Step 3: Write the client interface**

Create `internal/coderun/client.go`:

```go
package coderun

import "context"

// CodeRunClient is the boundary between the rest of the agent and however we
// happen to talk to CodeRun today. The current implementation drives a real
// browser; if stable internal endpoints are ever adopted, only the
// implementation behind this interface changes.
type CodeRunClient interface {
	ListSelections(ctx context.Context, group string) ([]Selection, error)
	ListProblems(ctx context.Context, selectionSlug string) ([]ProblemSummary, error)
	GetProblem(ctx context.Context, ref ProblemRef) (*Problem, int, error)
	GetTemplate(ctx context.Context, ref ProblemRef, compilerSlug string) (string, error)
	Submit(ctx context.Context, ref ProblemRef, compilerSlug string, sourcePath string) (*Submission, error)
	GetSubmission(ctx context.Context, globalID string) (*Submission, error)
}
```

- [ ] **Step 4: Write the crawler**

Create `internal/coderun/playwright/crawl.go`:

```go
package pwclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync"

	"github.com/playwright-community/playwright-go"

	"coderun-agent/internal/coderun"
	"coderun-agent/internal/coderun/extract"
)

// ProblemPath builds a problem URL. Passing a compilerSlug selects the
// language through the URL rather than through the dropdown.
func ProblemPath(ref coderun.ProblemRef, compilerSlug string) string {
	p := fmt.Sprintf("/selections/%s/problems/%s", ref.SelectionSlug, ref.ProblemSlug)
	if compilerSlug != "" {
		p += "?compiler=" + url.QueryEscape(compilerSlug)
	}
	return p
}

// ParseContextID pulls problemContextId out of a solution-template request URL.
// The id appears nowhere in the page HTML, so it is captured by observing the
// request the page makes on load.
func ParseContextID(raw string) (int, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return 0, fmt.Errorf("parse template url: %w", err)
	}
	v := u.Query().Get("problemContextId")
	if v == "" {
		return 0, fmt.Errorf("problemContextId absent from %q", raw)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("problemContextId %q is not a number: %w", v, err)
	}
	return n, nil
}

func (b *Browser) ListSelections(ctx context.Context, group string) ([]coderun.Selection, error) {
	if group == "" {
		group = "coderun-seasons"
	}
	html, err := b.Goto(ctx, "/selections?group="+url.QueryEscape(group))
	if err != nil {
		return nil, err
	}
	sels, err := extract.ParseSelections(html)
	if err != nil {
		return nil, err
	}
	for i := range sels {
		sels[i].Group = group
	}
	return sels, nil
}

// ListProblems walks every page of a selection's problem list.
func (b *Browser) ListProblems(ctx context.Context, selectionSlug string) ([]coderun.ProblemSummary, error) {
	var all []coderun.ProblemSummary
	seen := map[string]bool{}

	page, totalPages := 1, 1
	for page <= totalPages {
		path := fmt.Sprintf("/selections/%s", selectionSlug)
		if page > 1 {
			filters, err := extract.EncodeFilters(extract.DefaultFilters(page))
			if err != nil {
				return nil, err
			}
			path += "?filters=" + filters
		}

		html, err := b.Goto(ctx, path)
		if err != nil {
			return nil, err
		}
		probs, pages, err := extract.ParseProblemList(html, selectionSlug)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		if page == 1 {
			totalPages = pages
			slog.Info("problem list", "selection", selectionSlug, "pages", totalPages)

			// Guard against a silently missed pager. Page size is 20, so a
			// "single page" holding exactly 20 problems is far more likely to
			// be an undetected page 1 of N than a selection that happens to
			// end on the boundary. Under-crawling produces no error of its
			// own — whole pages simply never appear — so say so loudly.
			if totalPages == 1 && len(probs) >= extract.DefaultFilters(1).PageSize {
				slog.Warn("selection reports one page but is exactly full; the pager may not have been detected",
					"selection", selectionSlug, "problems", len(probs))
			}
		}

		added := 0
		for _, p := range probs {
			if seen[p.Ref.ProblemSlug] {
				continue
			}
			seen[p.Ref.ProblemSlug] = true
			all = append(all, p)
			added++
		}
		// If a page contributes nothing new, pagination is not working and
		// looping further would spin against the site.
		if page > 1 && added == 0 {
			return nil, fmt.Errorf("page %d returned no new problems: the ?filters= encoding may have drifted", page)
		}
		page++
	}
	return all, nil
}

// GetProblem loads a problem page, capturing the problemContextId from the
// solution-template request the page issues while it loads.
func (b *Browser) GetProblem(ctx context.Context, ref coderun.ProblemRef) (*coderun.Problem, int, error) {
	var (
		mu        sync.Mutex
		contextID int
	)
	handler := func(req playwright.Request) {
		if id, err := ParseContextID(req.URL()); err == nil {
			mu.Lock()
			contextID = id
			mu.Unlock()
		}
	}
	b.Page.On("request", handler)
	defer b.Page.RemoveListener("request", handler)

	html, err := b.Goto(ctx, ProblemPath(ref, ""))
	if err != nil {
		return nil, 0, err
	}

	// The template request fires during hydration, shortly after DOM ready.
	if err := b.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		slog.Debug("networkidle wait ended early", "error", err)
	}
	if fresh, err := b.Page.Content(); err == nil {
		html = fresh
	}

	problem, err := extract.ParseProblem(html, ref)
	if err != nil {
		return nil, 0, err
	}

	mu.Lock()
	id := contextID
	mu.Unlock()

	problem.Ref.ContextID = id
	return problem, id, nil
}

// GetTemplate fetches the starter code for a language. The entry-point name
// differs per language, so this is always fetched and never synthesised.
func (b *Browser) GetTemplate(ctx context.Context, ref coderun.ProblemRef, compilerSlug string) (string, error) {
	if ref.ContextID == 0 {
		return "", fmt.Errorf("context id unknown for %s: fetch the problem first", ref.ProblemSlug)
	}
	path := fmt.Sprintf("/api/problem/%s/solution-template?compilerSlug=%s&problemContextId=%d",
		url.PathEscape(ref.ProblemSlug), url.QueryEscape(compilerSlug), ref.ContextID)

	body, err := b.apiGet(ctx, path)
	if err != nil {
		return "", err
	}

	var payload struct {
		Result struct {
			Content string `json:"content"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode template response: %w", err)
	}
	return payload.Result.Content, nil
}

// apiGet issues a same-session request through the browser context, so cookies
// and origin match exactly what a normal user's browser would send.
func (b *Browser) apiGet(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	resp, err := b.Ctx.Request().Get(config.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("api GET %s: %w", path, err)
	}
	defer resp.Dispose()

	if resp.Status() != 200 {
		return nil, fmt.Errorf("api GET %s: status %d", path, resp.Status())
	}
	return resp.Body()
}
```

Note the import list for `crawl.go` must include `coderun-agent/internal/config` for the `config.BaseURL` reference in `apiGet`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: PASS, 12 tests

- [ ] **Step 6: Verify it compiles against the real API**

Run: `go build ./...`
Expected: no errors.

If `b.Ctx.Request()` does not exist in the installed playwright-go version, check the version's `BrowserContext` interface and use its APIRequestContext accessor. Do not fall back to a separate `net/http` client — that would break the single-session guarantee this design depends on.

- [ ] **Step 7: Commit**

```bash
git add internal/coderun/client.go internal/coderun/playwright/crawl.go internal/coderun/playwright/crawl_test.go
git commit -m "feat(crawl): list selections, paginate problems, extract statements and templates"
```

---

## Task 13: Submission via file upload

CodeRun is a **function-signature judge**: the submitted file must define the entry point named by that language's template (`solution` in Python, `solve` in Dart). It is not a stdin/stdout judge.

Submission goes through the upload modal, never the editor. `window.monaco` is undefined so `setValue()` is unavailable, and typing would let auto-indent corrupt whitespace-significant languages.

**Files:**
- Create: `internal/coderun/playwright/submit.go`
- Test: `internal/coderun/playwright/submit_test.go`

**Interfaces:**
- Consumes: `Browser`, `ProblemPath` (Task 12), `coderun.Submission` (Task 3)
- Produces:
  - `pwclient.ValidateSource(source []byte, maxBytes int64) error` — pure
  - `pwclient.ParseSubmitResponse(body []byte) (globalID, status string, err error)` — pure
  - `(*Browser).Submit(ctx context.Context, ref coderun.ProblemRef, compilerSlug, sourcePath string) (*coderun.Submission, error)` — the size limit comes from `Browser.maxSource`, set in `New`, so the signature matches `coderun.CodeRunClient`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/playwright/submit_test.go`:

```go
package pwclient

import (
	"strings"
	"testing"
)

func TestValidateSourceAcceptsNormalFile(t *testing.T) {
	if err := ValidateSource([]byte("def solution(n, a):\n    return 0\n"), 262144); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSourceRejectsEmpty(t *testing.T) {
	if err := ValidateSource(nil, 262144); err == nil {
		t.Fatal("expected an error for an empty source file")
	}
}

func TestValidateSourceRejectsOversize(t *testing.T) {
	// CodeRun's upload modal states a 256 KB limit. Catch it locally rather
	// than letting the site reject the upload.
	big := make([]byte, 262145)
	for i := range big {
		big[i] = 'x'
	}
	err := ValidateSource(big, 262144)
	if err == nil {
		t.Fatal("expected an error for a file above the 256 KB limit")
	}
	if !strings.Contains(err.Error(), "262144") {
		t.Errorf("error should state the limit, got %v", err)
	}
}

func TestParseSubmitResponse(t *testing.T) {
	// Captured verbatim from a real POST /api/submission/submit response.
	body := []byte(`{"result":{"id":2338530,` +
		`"globalId":"1001006a-7f48-105c-b62a-db256d869399","status":"PENDING",` +
		`"source":[{"key":"coding/solutions/probe.py","bucket":"contest-hidden",` +
		`"contentType":"text/x-python","size":65,"fileName":"probe.py"}]},"error":null}`)

	globalID, status, err := ParseSubmitResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if globalID != "1001006a-7f48-105c-b62a-db256d869399" {
		t.Errorf("globalID = %q", globalID)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING", status)
	}
}

func TestParseSubmitResponseSurfacesAPIError(t *testing.T) {
	body := []byte(`{"result":null,"error":{"statusCode":403,"code":"bad-csrf","message":"Invalid CSRF Token"}}`)
	if _, _, err := ParseSubmitResponse(body); err == nil {
		t.Fatal("expected an error when the API returns one")
	}
}

func TestParseSubmitResponseRejectsMissingGlobalID(t *testing.T) {
	// A submission we cannot identify must never be reported as successful:
	// we would have no way to poll it and might blindly resubmit.
	body := []byte(`{"result":{"id":1,"status":"PENDING"},"error":null}`)
	if _, _, err := ParseSubmitResponse(body); err == nil {
		t.Fatal("expected an error when globalId is absent")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/playwright/ -run 'ValidateSource|SubmitResponse' -v`
Expected: FAIL — `undefined: ValidateSource`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/playwright/submit.go`:

```go
package pwclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"coderun-agent/internal/coderun"
)

// ValidateSource enforces CodeRun's stated upload limit locally.
func ValidateSource(source []byte, maxBytes int64) error {
	if len(source) == 0 {
		return fmt.Errorf("source file is empty")
	}
	if int64(len(source)) > maxBytes {
		return fmt.Errorf("source file is %d bytes, limit is %d", len(source), maxBytes)
	}
	return nil
}

// ParseSubmitResponse reads the POST /api/submission/submit response.
func ParseSubmitResponse(body []byte) (string, string, error) {
	var payload struct {
		Result *struct {
			GlobalID string `json:"globalId"`
			Status   string `json:"status"`
		} `json:"result"`
		Error *struct {
			StatusCode int    `json:"statusCode"`
			Code       string `json:"code"`
			Message    string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("decode submit response: %w", err)
	}
	if payload.Error != nil {
		return "", "", fmt.Errorf("submit rejected: %s (%s, status %d)",
			payload.Error.Message, payload.Error.Code, payload.Error.StatusCode)
	}
	if payload.Result == nil || payload.Result.GlobalID == "" {
		return "", "", fmt.Errorf("submit response carried no globalId; cannot track this submission")
	}
	return payload.Result.GlobalID, payload.Result.Status, nil
}

// Submit uploads a source file through the attach-file modal.
//
// The response listener is registered before the confirming click so the
// globalId is captured exactly, with no race against the redirect that
// follows.
func (b *Browser) Submit(ctx context.Context, ref coderun.ProblemRef, compilerSlug, sourcePath string) (*coderun.Submission, error) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	if err := ValidateSource(source, b.maxSource); err != nil {
		return nil, err
	}

	if _, err := b.Goto(ctx, ProblemPath(ref, compilerSlug)); err != nil {
		return nil, err
	}

	if err := b.Page.GetByTestId("file-attach").Click(); err != nil {
		return nil, fmt.Errorf("open the upload dialog: %w", err)
	}

	input := b.Page.Locator(`[data-testid="file-attach-modal"] input[type=file]`)
	if err := input.SetInputFiles([]string{sourcePath}); err != nil {
		return nil, fmt.Errorf("attach %s: %w", sourcePath, err)
	}

	submittedAt := time.Now().UTC()

	resp, err := b.Page.ExpectResponse("**/api/submission/submit", func() error {
		return b.Page.GetByTestId("confirm").Click()
	})
	if err != nil {
		// The click may have landed even though the response was missed. Do
		// not retry: a blind resubmit could double-submit.
		return nil, fmt.Errorf("submit sent but the response was not captured (do not retry blindly): %w", err)
	}

	body, err := resp.Body()
	if err != nil {
		return nil, fmt.Errorf("read submit response: %w", err)
	}
	globalID, status, err := ParseSubmitResponse(body)
	if err != nil {
		return nil, err
	}

	slog.Info("submitted", "problem", ref.ProblemSlug, "compiler", compilerSlug, "submission", globalID)

	return &coderun.Submission{
		GlobalID:     globalID,
		Ref:          ref,
		CompilerSlug: compilerSlug,
		Status:       status,
		SubmittedAt:  submittedAt,
	}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: PASS, 18 tests

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/playwright/submit.go internal/coderun/playwright/submit_test.go
git commit -m "feat(submit): upload solutions through the attach-file modal"
```

---

## Task 14: Verdict polling and test artifacts

**Files:**
- Create: `internal/coderun/playwright/verdict.go`
- Test: `internal/coderun/playwright/verdict_test.go`

**Interfaces:**
- Consumes: `Browser.apiGet` (Task 12), `coderun.Submission` (Task 3)
- Produces:
  - `pwclient.ParseSubmissionDetail(body []byte) (*coderun.Submission, []string, error)` — the `[]string` is the presigned links for the first failing sample test, in `input, output, answer` order
  - `(*Browser).GetSubmission(ctx, globalID string) (*coderun.Submission, error)`
  - `(*Browser).AwaitVerdict(ctx, globalID string, interval, timeout time.Duration) (*coderun.Submission, error)`
  - `(*Browser).FetchTestArtifacts(ctx, sub *coderun.Submission, links []string, maxBytes int64) error`

- [ ] **Step 1: Write the failing test**

Create `internal/coderun/playwright/verdict_test.go`:

```go
package pwclient

import (
	"testing"

	"coderun-agent/internal/coderun"
)

// Captured verbatim from GET /api/submission/<globalId>, with the presigned
// URLs shortened. Presigned links are bearer credentials and are never stored.
const detailJSON = `{"result":{
 "globalId":"1001006a-7f48-105c-b62a-db256d869399",
 "verdict":"WRONG_ANSWER","status":"FINISHED",
 "submitAt":"2026-08-14T16:53:35.989206Z",
 "compiler":{"slug":"python_make","title":"Python","version":"3.12.3"},
 "maxMemoryUsageBytes":3620864,"maxTimeUsageMillis":23,
 "firstFailedTestNumber":1,"compileLog":null,
 "openTests":{"totalTests":2,"tests":[
   {"testNumber":1,"usedTimeMillis":23,"usedMemoryBytes":3620864,
    "isSample":true,"verdict":"WRONG_ANSWER",
    "input":{"link":"https://s3/input","size":13},
    "output":{"link":"https://s3/output","size":2},
    "answer":{"link":"https://s3/answer","size":2}}]},
 "hiddenTests":{"totalTests":83,"tests":[]},
 "runtimeLimits":{"timeLimitMillis":2000,"memoryLimitBytes":268435456},
 "isExpired":false},"error":null}`

func TestParseSubmissionDetail(t *testing.T) {
	sub, links, err := ParseSubmissionDetail([]byte(detailJSON))
	if err != nil {
		t.Fatal(err)
	}

	if sub.Verdict != "WRONG_ANSWER" {
		t.Errorf("Verdict = %q", sub.Verdict)
	}
	if sub.Status != "FINISHED" {
		t.Errorf("Status = %q", sub.Status)
	}
	if sub.MaxTimeMillis != 23 {
		t.Errorf("MaxTimeMillis = %d, want 23", sub.MaxTimeMillis)
	}
	if sub.MaxMemoryBytes != 3620864 {
		t.Errorf("MaxMemoryBytes = %d", sub.MaxMemoryBytes)
	}
	if sub.FirstFailedTest != 1 {
		t.Errorf("FirstFailedTest = %d, want 1", sub.FirstFailedTest)
	}
	if sub.HiddenTestCount != 83 {
		t.Errorf("HiddenTestCount = %d, want 83", sub.HiddenTestCount)
	}
	if sub.TimeLimitMillis != 2000 || sub.MemoryLimitBytes != 268435456 {
		t.Errorf("runtime limits = %d ms / %d bytes", sub.TimeLimitMillis, sub.MemoryLimitBytes)
	}
	if len(sub.OpenTests) != 1 {
		t.Fatalf("got %d open tests, want 1", len(sub.OpenTests))
	}
	if !sub.OpenTests[0].IsSample {
		t.Error("open test should be marked as a sample")
	}
	if len(links) != 3 {
		t.Fatalf("got %d artifact links, want 3 (input, output, answer)", len(links))
	}
	if links[0] != "https://s3/input" || links[2] != "https://s3/answer" {
		t.Errorf("links out of order: %v", links)
	}
}

func TestParseSubmissionDetailPending(t *testing.T) {
	body := []byte(`{"result":{"globalId":"g","status":"PENDING","verdict":""},"error":null}`)
	sub, links, err := ParseSubmissionDetail(body)
	if err != nil {
		t.Fatal(err)
	}
	if coderun.IsFinished(sub.Status) {
		t.Error("PENDING must not be reported as finished")
	}
	if len(links) != 0 {
		t.Error("a pending submission has no artifacts to fetch")
	}
}

func TestIsAcceptedIsClosedOnSuccessOnly(t *testing.T) {
	// The open-set rule: only known-success verdicts pass. Everything else,
	// including verdicts nobody has seen yet, is failure.
	for _, v := range []string{"OK", "ACCEPTED"} {
		if !coderun.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = false, want true", v)
		}
	}
	for _, v := range []string{
		"WRONG_ANSWER", "TIME_LIMIT_EXCEEDED", "COMPILATION_ERROR",
		"RUNTIME_ERROR", "SOMETHING_NOBODY_HAS_SEEN_YET", "", "ok", "Accepted",
	} {
		if coderun.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = true — unknown verdicts must never count as success", v)
		}
	}
}

func TestParseSubmissionDetailSurfacesAPIError(t *testing.T) {
	body := []byte(`{"result":null,"error":{"statusCode":404,"code":"not-found","message":"nope"}}`)
	if _, _, err := ParseSubmissionDetail(body); err == nil {
		t.Fatal("expected an error when the API returns one")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/coderun/playwright/ -run 'SubmissionDetail|IsAccepted' -v`
Expected: FAIL — `undefined: ParseSubmissionDetail`

- [ ] **Step 3: Write the implementation**

Create `internal/coderun/playwright/verdict.go`:

```go
package pwclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"coderun-agent/internal/coderun"
)

type fileRef struct {
	Link string `json:"link"`
	Size int64  `json:"size"`
}

type detailPayload struct {
	Result *struct {
		GlobalID            string `json:"globalId"`
		Verdict             string `json:"verdict"`
		Status              string `json:"status"`
		SubmitAt            string `json:"submitAt"`
		MaxMemoryUsageBytes int64  `json:"maxMemoryUsageBytes"`
		MaxTimeUsageMillis  int    `json:"maxTimeUsageMillis"`
		FirstFailedTest     int    `json:"firstFailedTestNumber"`
		CompileLog          string `json:"compileLog"`
		Compiler            struct {
			Slug string `json:"slug"`
		} `json:"compiler"`
		OpenTests struct {
			TotalTests int `json:"totalTests"`
			Tests      []struct {
				TestNumber      int     `json:"testNumber"`
				UsedTimeMillis  int     `json:"usedTimeMillis"`
				UsedMemoryBytes int64   `json:"usedMemoryBytes"`
				IsSample        bool    `json:"isSample"`
				Verdict         string  `json:"verdict"`
				Input           fileRef `json:"input"`
				Output          fileRef `json:"output"`
				Answer          fileRef `json:"answer"`
			} `json:"tests"`
		} `json:"openTests"`
		HiddenTests struct {
			TotalTests int `json:"totalTests"`
		} `json:"hiddenTests"`
		RuntimeLimits struct {
			TimeLimitMillis  int   `json:"timeLimitMillis"`
			MemoryLimitBytes int64 `json:"memoryLimitBytes"`
		} `json:"runtimeLimits"`
	} `json:"result"`
	Error *struct {
		StatusCode int    `json:"statusCode"`
		Code       string `json:"code"`
		Message    string `json:"message"`
	} `json:"error"`
}

// ParseSubmissionDetail decodes GET /api/submission/<globalId>.
//
// It returns the submission plus the presigned links for the first failing
// sample test, in input/output/answer order. Those links are returned rather
// than stored: they are bearer credentials valid for 12 hours and must never
// be persisted or logged.
func ParseSubmissionDetail(body []byte) (*coderun.Submission, []string, error) {
	var p detailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, nil, fmt.Errorf("decode submission detail: %w", err)
	}
	if p.Error != nil {
		return nil, nil, fmt.Errorf("submission detail: %s (%s, status %d)",
			p.Error.Message, p.Error.Code, p.Error.StatusCode)
	}
	if p.Result == nil {
		return nil, nil, fmt.Errorf("submission detail carried no result")
	}
	r := p.Result

	sub := &coderun.Submission{
		GlobalID:         r.GlobalID,
		CompilerSlug:     r.Compiler.Slug,
		Status:           r.Status,
		Verdict:          r.Verdict,
		MaxTimeMillis:    r.MaxTimeUsageMillis,
		MaxMemoryBytes:   r.MaxMemoryUsageBytes,
		FirstFailedTest:  r.FirstFailedTest,
		CompileLog:       r.CompileLog,
		TimeLimitMillis:  r.RuntimeLimits.TimeLimitMillis,
		MemoryLimitBytes: r.RuntimeLimits.MemoryLimitBytes,
		HiddenTestCount:  r.HiddenTests.TotalTests,
	}
	if t, err := time.Parse(time.RFC3339Nano, r.SubmitAt); err == nil {
		sub.SubmittedAt = t
	}

	var links []string
	for _, t := range r.OpenTests.Tests {
		sub.OpenTests = append(sub.OpenTests, coderun.TestResult{
			Number:      t.TestNumber,
			Verdict:     t.Verdict,
			IsSample:    t.IsSample,
			TimeMillis:  t.UsedTimeMillis,
			MemoryBytes: t.UsedMemoryBytes,
		})
		// Only the first failing test's artifacts are needed: it is what a
		// correction step would reason about.
		if links == nil && !coderun.IsAccepted(t.Verdict) && t.Verdict != "" {
			links = []string{t.Input.Link, t.Output.Link, t.Answer.Link}
		}
	}
	return sub, links, nil
}

func (b *Browser) GetSubmission(ctx context.Context, globalID string) (*coderun.Submission, error) {
	body, err := b.apiGet(ctx, "/api/submission/"+globalID)
	if err != nil {
		return nil, err
	}
	sub, _, err := ParseSubmissionDetail(body)
	return sub, err
}

// AwaitVerdict polls until judging finishes. It observes API state rather than
// reloading the page, per the design's rate-limiting rules.
func (b *Browser) AwaitVerdict(ctx context.Context, globalID string, interval, timeout time.Duration) (*coderun.Submission, error) {
	deadline := time.Now().Add(timeout)

	for {
		body, err := b.apiGet(ctx, "/api/submission/"+globalID)
		if err != nil {
			return nil, err
		}
		sub, links, err := ParseSubmissionDetail(body)
		if err != nil {
			return nil, err
		}

		if coderun.IsFinished(sub.Status) {
			if !coderun.IsAccepted(sub.Verdict) && !knownVerdict(sub.Verdict) {
				// Not an error — but a verdict we have never seen is worth a
				// loud line, because the open-set rule just classified it as
				// a failure.
				slog.Warn("unrecognised verdict; treated as not accepted",
					"verdict", sub.Verdict, "submission", globalID)
			}
			if len(links) > 0 {
				if err := b.FetchTestArtifacts(ctx, sub, links, b.maxArtifact); err != nil {
					slog.Warn("could not fetch failing-test artifacts", "error", err)
				}
			}
			return sub, nil
		}

		if time.Now().After(deadline) {
			return sub, fmt.Errorf("timed out after %s waiting for a verdict (last status %q)", timeout, sub.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// knownVerdict lists verdicts observed or strongly expected. It exists only to
// decide whether to log a warning — never to decide success.
func knownVerdict(v string) bool {
	switch v {
	case "WRONG_ANSWER", "TIME_LIMIT_EXCEEDED", "MEMORY_LIMIT_EXCEEDED",
		"RUNTIME_ERROR", "COMPILATION_ERROR", "PRESENTATION_ERROR":
		return true
	}
	return false
}

// FetchTestArtifacts downloads the failing sample test's input, actual output
// and expected answer. Links expire after 12 hours, so this runs immediately
// and the links themselves are never stored.
func (b *Browser) FetchTestArtifacts(ctx context.Context, sub *coderun.Submission, links []string, maxBytes int64) error {
	if len(sub.OpenTests) == 0 || len(links) < 3 {
		return nil
	}
	fields := []*string{}
	for i := range sub.OpenTests {
		if !coderun.IsAccepted(sub.OpenTests[i].Verdict) && sub.OpenTests[i].Verdict != "" {
			t := &sub.OpenTests[i]
			fields = []*string{&t.Input, &t.Output, &t.Answer}
			break
		}
	}
	if len(fields) != 3 {
		return nil
	}

	for i, link := range links[:3] {
		if link == "" {
			continue
		}
		text, err := b.fetchArtifact(ctx, link, maxBytes)
		if err != nil {
			return fmt.Errorf("artifact %d: %w", i, err)
		}
		*fields[i] = text
	}
	return nil
}

func (b *Browser) fetchArtifact(ctx context.Context, link string, maxBytes int64) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Note: the URL is deliberately absent from this log line. Presigned links
	// are credentials.
	slog.Debug("fetching test artifact", "url", "<presigned>")

	resp, err := b.Ctx.Request().Get(link)
	if err != nil {
		return "", fmt.Errorf("fetch <presigned>: %w", err)
	}
	defer resp.Dispose()

	if resp.Status() != 200 {
		return "", fmt.Errorf("fetch <presigned>: status %d", resp.Status())
	}
	raw, err := resp.Body()
	if err != nil {
		return "", err
	}
	if int64(len(raw)) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/coderun/playwright/ -v`
Expected: PASS, 22 tests

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/playwright/verdict.go internal/coderun/playwright/verdict_test.go
git commit -m "feat(verdict): poll submissions and capture failing-test artifacts"
```

---

## Task 15: CLI

**Files:**
- Create: `cmd/coderun-agent/main.go`, `cmd/coderun-agent/auth.go`, `cmd/coderun-agent/crawl.go`, `cmd/coderun-agent/submit.go`, `cmd/coderun-agent/status.go`
- Test: `cmd/coderun-agent/lang_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–14
- Produces: the `coderun-agent` binary; `resolveCompiler(name string, compilers []coderun.Compiler) (string, error)`

- [ ] **Step 1: Add cobra**

```bash
go get github.com/spf13/cobra
```

- [ ] **Step 2: Write the failing test**

Create `cmd/coderun-agent/lang_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"coderun-agent/internal/coderun"
)

var testCompilers = []coderun.Compiler{
	{Slug: "python_make", Title: "Python", Version: "3.12.3"},
	{Slug: "nodejs_20_make", Title: "JavaScript", Version: "20.14.0"},
	{Slug: "cpp_make", Title: "C++", Version: "14.1.0"},
}

func TestResolveCompilerBySlug(t *testing.T) {
	got, err := resolveCompiler("python_make", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "python_make" {
		t.Errorf("got %q", got)
	}
}

func TestResolveCompilerByFriendlyName(t *testing.T) {
	got, err := resolveCompiler("python", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "python_make" {
		t.Errorf("got %q, want python_make", got)
	}
}

func TestResolveCompilerJavaScriptMapsToNodeSlug(t *testing.T) {
	// The slug is nodejs_20_make. Deriving it from the name is impossible,
	// which is exactly why resolution goes through the scraped list.
	got, err := resolveCompiler("javascript", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "nodejs_20_make" {
		t.Errorf("got %q, want nodejs_20_make", got)
	}
}

func TestResolveCompilerUnknownListsOptions(t *testing.T) {
	_, err := resolveCompiler("brainfuck", testCompilers)
	if err == nil {
		t.Fatal("expected an error for an unknown language")
	}
	// The error must be actionable rather than merely negative.
	if !strings.Contains(err.Error(), "python_make") {
		t.Errorf("error should list the available compilers, got %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./cmd/coderun-agent/ -v`
Expected: FAIL — `undefined: resolveCompiler`

- [ ] **Step 4: Write the root command**

Create `cmd/coderun-agent/main.go`:

```go
// Command coderun-agent automates Yandex CodeRun through a real browser.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

// Compile-time proof that the browser implementation satisfies the interface.
var _ coderun.CodeRunClient = (*pwclient.Browser)(nil)

var verbose bool

func main() {
	root := &cobra.Command{
		Use:   "coderun-agent",
		Short: "Automate Yandex CodeRun through a real browser",
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
				&slog.HandlerOptions{Level: level})))
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "debug logging")

	root.AddCommand(authCmd(), selectionsCmd(), problemsCmd(), problemCmd(),
		submitCmd(), statusCmd(), notImplemented("solve", "generate and submit a solution"),
		notImplemented("solution", "show the accepted solution"))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// notImplemented registers commands that belong to the LLM milestone, so the
// CLI surface is honest about what exists.
func notImplemented(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short + " (not implemented in this milestone)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%s is not implemented in this milestone", name)
		},
	}
}

// withBrowser wires config, storage and browser, and guarantees cleanup.
func withBrowser(headed bool, fn func(ctx context.Context, cfg *config.Config, b *pwclient.Browser, st *storage.Store) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	b, err := pwclient.New(cfg, headed)
	if err != nil {
		return err
	}
	defer b.Close()

	return fn(context.Background(), cfg, b, st)
}

// resolveCompiler turns a user-supplied language name into a compilerSlug
// using the list scraped from the page. Slugs are never derived from names:
// JavaScript's is nodejs_20_make.
func resolveCompiler(name string, compilers []coderun.Compiler) (string, error) {
	want := strings.ToLower(strings.TrimSpace(name))

	for _, c := range compilers {
		if strings.ToLower(c.Slug) == want {
			return c.Slug, nil
		}
	}
	for _, c := range compilers {
		if strings.ToLower(c.Title) == want {
			return c.Slug, nil
		}
	}
	// Last resort: a prefix match, so "c++" finds "C++ 14.1.0".
	for _, c := range compilers {
		if strings.HasPrefix(strings.ToLower(c.Title), want) && want != "" {
			return c.Slug, nil
		}
	}

	var available []string
	for _, c := range compilers {
		available = append(available, fmt.Sprintf("%s (%s)", c.Slug, c.Title))
	}
	return "", fmt.Errorf("unknown language %q; available: %s", name, strings.Join(available, ", "))
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./cmd/coderun-agent/ -v`
Expected: PASS, 4 tests

- [ ] **Step 6: Write the auth commands**

Create `cmd/coderun-agent/auth.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage the browser session"}

	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Open a browser and wait for you to log into Yandex",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Always headed: logging in is inherently interactive.
			return withBrowser(true, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, _ *storage.Store) error {
				return b.AwaitLogin(ctx, 5*time.Minute)
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report whether the stored session is still valid",
		RunE: func(_ *cobra.Command, _ []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, _ *storage.Store) error {
				ok, err := b.AuthStatus(ctx)
				if err != nil {
					return err
				}
				if ok {
					fmt.Println("authenticated")
					return nil
				}
				return fmt.Errorf("not authenticated — run `coderun-agent auth login`")
			})
		},
	})
	return cmd
}
```

- [ ] **Step 7: Write the crawl commands**

Create `cmd/coderun-agent/crawl.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func selectionsCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "selections",
		Short: "List CodeRun selections",
		RunE: func(_ *cobra.Command, _ []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				sels, err := b.ListSelections(ctx, group)
				if err != nil {
					return err
				}
				if err := st.UpsertSelections(ctx, sels); err != nil {
					return err
				}
				for i, s := range sels {
					fmt.Printf("%2d. %-34s %s\n", i+1, s.Slug, s.Title)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "coderun-seasons", "selection group")
	return cmd
}

func problemsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "problems <selection>",
		Short: "List a selection's problems across all pages",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				probs, err := b.ListProblems(ctx, args[0])
				if err != nil {
					return err
				}
				if err := st.UpsertProblems(ctx, probs); err != nil {
					return err
				}
				for _, p := range probs {
					fmt.Printf("%3d. %-40s %-10s %s\n", p.Number, p.Ref.ProblemSlug, p.Difficulty.Raw, p.Title)
				}
				fmt.Printf("\n%d problems\n", len(probs))
				return nil
			})
		},
	}
}

func problemCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "problem <selection> <slug>",
		Short: "Fetch and display one problem",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, _ *config.Config, b *pwclient.Browser, st *storage.Store) error {
				ref := coderun.ProblemRef{SelectionSlug: args[0], ProblemSlug: args[1]}

				p, contextID, err := b.GetProblem(ctx, ref)
				if err != nil {
					return err
				}
				if err := st.SaveProblem(ctx, p); err != nil {
					return err
				}
				if contextID != 0 {
					if err := st.SetContextID(ctx, p.Ref); err != nil {
						return err
					}
				}

				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				}
				fmt.Printf("%d. %s  [%s]\n\n%s\n\n", p.Number, p.Title, p.Difficulty.Raw, p.Statement)
				fmt.Printf("Формат ввода\n%s\n\nФормат вывода\n%s\n", p.InputFormat, p.OutputFormat)
				for i, ex := range p.Examples {
					fmt.Printf("\nExample %d\nInput:\n%s\nOutput:\n%s\n", i+1, ex.Input, ex.Output)
				}
				fmt.Printf("\nLanguages: %d\n", len(p.Languages))
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full problem as JSON")
	return cmd
}
```

- [ ] **Step 8: Write the submit and status commands**

Create `cmd/coderun-agent/submit.go`:

```go
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	pwclient "coderun-agent/internal/coderun/playwright"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func submitCmd() *cobra.Command {
	var file, lang string

	cmd := &cobra.Command{
		Use:   "submit <selection> <slug>",
		Short: "Submit a source file and wait for the verdict",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withBrowser(false, func(ctx context.Context, cfg *config.Config, b *pwclient.Browser, st *storage.Store) error {
				ref := coderun.ProblemRef{SelectionSlug: args[0], ProblemSlug: args[1]}

				fmt.Println("[1/4] Fetching problem...")
				p, contextID, err := b.GetProblem(ctx, ref)
				if err != nil {
					return err
				}
				ref.ContextID = contextID
				if err := st.SaveProblem(ctx, p); err != nil {
					return err
				}
				if contextID != 0 {
					if err := st.SetContextID(ctx, ref); err != nil {
						return err
					}
				}

				slug, err := resolveCompiler(lang, p.Languages)
				if err != nil {
					return err
				}

				fmt.Printf("[2/4] Submitting %s as %s...\n", file, slug)
				sub, err := b.Submit(ctx, ref, slug, file)
				if err != nil {
					return err
				}
				fmt.Printf("      submission %s\n", sub.GlobalID)

				attempt, err := st.NextAttempt(ctx, ref.SelectionSlug, ref.ProblemSlug)
				if err != nil {
					return err
				}

				fmt.Println("[3/4] Waiting for the verdict...")
				final, err := b.AwaitVerdict(ctx, sub.GlobalID, cfg.PollInterval, cfg.SubmissionTimeout)
				if err != nil {
					return err
				}
				final.Ref = ref

				fmt.Println("[4/4] Recording the attempt...")
				source, err := readFile(file)
				if err != nil {
					return err
				}
				ext := strings.TrimPrefix(filepath.Ext(file), ".")
				if err := storage.WriteAttempt("solutions", ref, attempt, ext, source, storage.AttemptMeta{
					Language:     slug,
					SubmissionID: final.GlobalID,
					Verdict:      final.Verdict,
				}); err != nil {
					return err
				}

				printVerdict(final)
				if !coderun.IsAccepted(final.Verdict) {
					return fmt.Errorf("not accepted: %s", final.Verdict)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "path to the source file (required)")
	cmd.Flags().StringVar(&lang, "lang", "python", "language name or compiler slug")
	cmd.MarkFlagRequired("file")
	return cmd
}

func printVerdict(s *coderun.Submission) {
	fmt.Printf("\nVerdict: %s\n", s.Verdict)
	fmt.Printf("Time:    %d ms (limit %d ms)\n", s.MaxTimeMillis, s.TimeLimitMillis)
	fmt.Printf("Memory:  %d bytes (limit %d bytes)\n", s.MaxMemoryBytes, s.MemoryLimitBytes)

	if s.CompileLog != "" {
		fmt.Printf("\nCompile log:\n%s\n", s.CompileLog)
	}
	if coderun.IsAccepted(s.Verdict) {
		return
	}
	if s.FirstFailedTest > 0 {
		fmt.Printf("\nFirst failed test: %d (of %d sample + %d hidden)\n",
			s.FirstFailedTest, len(s.OpenTests), s.HiddenTestCount)
	}
	for _, t := range s.OpenTests {
		if coderun.IsAccepted(t.Verdict) || t.Input == "" {
			continue
		}
		fmt.Printf("\nTest %d — %s\nInput:\n%s\nExpected:\n%s\nActual:\n%s\n",
			t.Number, t.Verdict, t.Input, t.Answer, t.Output)
	}
}
```

Create `cmd/coderun-agent/status.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"coderun-agent/internal/coderun"
	"coderun-agent/internal/config"
	"coderun-agent/internal/storage"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show locally recorded progress",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			st, err := storage.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()

			counts, err := st.Counts(context.Background())
			if err != nil {
				return err
			}
			total := 0
			for _, n := range counts {
				total += n
			}
			fmt.Printf("Total:      %d\n", total)
			fmt.Printf("Accepted:   %d\n", counts[coderun.StatusSolved])
			fmt.Printf("Attempted:  %d\n", counts[coderun.StatusWrong])
			fmt.Printf("Unsolved:   %d\n", counts[coderun.StatusNotSolved])
			return nil
		},
	}
}
```

Create `cmd/coderun-agent/util.go`:

```go
package main

import "os"

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
```

- [ ] **Step 9: Build and verify the whole tree**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: builds clean, vet clean, all tests pass with no browser and no network.

The `var _ coderun.CodeRunClient = (*pwclient.Browser)(nil)` line in `main.go` is the compile-time check that every method signature lines up. If it fails, fix the implementation to match the interface rather than loosening the interface.

- [ ] **Step 10: Commit**

```bash
git add cmd/coderun-agent/
git commit -m "feat(cli): add auth, crawl, submit and status commands"
```

---

## Task 16: Integration tests

These touch the live site and are excluded from the default suite. They are run deliberately, never in CI.

**Files:**
- Create: `internal/coderun/playwright/integration_test.go`
- Modify: `README.md` (create)

**Interfaces:**
- Consumes: everything
- Produces: no production code

- [ ] **Step 1: Write the integration tests**

Create `internal/coderun/playwright/integration_test.go`:

```go
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
```

- [ ] **Step 2: Add the test helper**

Append to `internal/coderun/playwright/crawl.go`:

```go
// ProblemRefFor is a small constructor used by tests and callers that only
// have the two slugs.
func ProblemRefFor(selectionSlug, problemSlug string) coderun.ProblemRef {
	return coderun.ProblemRef{SelectionSlug: selectionSlug, ProblemSlug: problemSlug}
}
```

- [ ] **Step 3: Verify the default suite still excludes them**

Run: `go test ./...`
Expected: PASS, and no browser launches.

Run: `go test -tags integration ./internal/coderun/playwright/ -v`
Expected: the crawl tests pass; `TestIntegrationAuthStatus` skips unless you have logged in.

- [ ] **Step 4: Write the README**

Create `README.md`:

```markdown
# coderun-agent

Automates Yandex CodeRun through a real browser. Milestone 1: crawl, extract,
and submit a manually supplied source file. No LLM yet.

## Setup

    go build ./...
    go run github.com/playwright-community/playwright-go/cmd/playwright@latest install chromium --with-deps
    cp .env.example .env

## Usage

    coderun-agent auth login                  # opens a browser; log in yourself
    coderun-agent auth status
    coderun-agent selections
    coderun-agent problems 2025-summer-common
    coderun-agent problem 2025-summer-common bridge-to-the-palace
    coderun-agent submit 2025-summer-common bridge-to-the-palace --file sol.py --lang python
    coderun-agent status

## Writing a solution

CodeRun judges **functions, not stdin/stdout**. Your file must define the entry
point from that language's template — `solution` in Python, `solve` in Dart.
Fetch it with `coderun-agent problem <sel> <slug> --json` and read `Languages`.

    def solution(n, a):
        ...

## Notes

- Crawling needs no login. Only status and submission do.
- The session lives in `.browser/`. Never commit it.
- The agent stops on CAPTCHAs and never tries to work around them.
- Submissions count toward your CodeRun record.

## Tests

    go test ./...                                          # offline, no browser
    go test -tags integration ./internal/coderun/playwright/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/coderun/playwright/integration_test.go internal/coderun/playwright/crawl.go README.md
git commit -m "test: add live integration tests behind a build tag, and a README"
```

---

## Self-Review

Checked after writing, and issues fixed inline:

- **Fixed:** `coderun.Submission` and `TestResult` were referenced by `client.go` in Task 12 but never defined. Added to `models.go` in Task 3, together with `IsAccepted` and `IsFinished`.
- **Fixed:** `Browser.Submit` took a `maxBytes` parameter that `CodeRunClient` did not, so the interface could never be satisfied. Moved the limit onto the `Browser` struct, set from config in `New`.
- **Fixed:** Task 13 contained a placeholder `var _ playwright.Page` line with an instruction to delete it, which would have left an unused import. Removed both.
- **Fixed:** `FetchTestArtifacts` was called with a hardcoded `262144`; now uses `Browser.maxArtifact` from config.

**Spec coverage.** Every section of the design maps to a task: §2 architecture → Tasks 10–15; §3 identity/types → Task 3; §4 auth → Task 11; §5 crawling and pagination → Tasks 5, 6, 12; §6 statement/KaTeX → Tasks 4, 7; §7 languages/templates → Tasks 7, 12, 15; §8 submission → Task 13; §9 polling and open-set verdicts → Task 14; §10 storage → Tasks 8, 9; §11 CLI → Task 15; §12 safety → Tasks 10, 15; §13 config → Task 1; §14 testing → Tasks 2–16.

**Known deferrals, deliberate:** the design's `export` command (§11) is not implemented — `problem --json` covers the need, and a bulk exporter with no consumer would be speculative. `storage.UnmarshalProblem` exists for it when a consumer appears.

## Definition of Done

- `go test ./...` passes with no browser and no network access.
- `go build ./... && go vet ./...` are clean.
- `auth login` establishes a session that survives a process restart.
- `selections` lists 13 seasonal selections.
- `problems 2025-summer-common` returns more than 20 problems, proving pagination.
- `problem 2025-summer-common bridge-to-the-palace` shows constraints containing
  `$1 \le N \le 10^5$` and **not** doubled text, with both examples.
- `submit … --file sol.py --lang python` submits, polls, prints a verdict with
  per-test detail, and writes `solutions/<sel>/<slug>/attempt-01.py` plus its sidecar.
