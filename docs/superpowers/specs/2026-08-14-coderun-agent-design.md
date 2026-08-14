# CodeRun Automation Agent — Design (Milestone 1)

Date: 2026-08-14
Scope: PLAN.md phases 1–6. Ends at `coderun-agent submit <id> --file solution.py`.
LLM integration (PLAN.md §12–§16, phases 7–10) is explicitly **out of scope** and
will be a separate spec.

Grounded in live observation recorded in [`docs/coderun-research.md`](../../coderun-research.md).
Where this document contradicts PLAN.md, the research doc is the reason; each such
departure is called out under "Departures from PLAN.md".

## 1. Goal

Prove, in working code, that the agent can:

1. Authenticate to CodeRun via a real browser and persist that session.
2. Discover selections and their problems, including pagination.
3. Extract a complete, faithful problem statement.
4. Submit a manually supplied source file.
5. Poll for and record the verdict, including per-test failure detail.

This is PLAN.md §24's stated first objective. No LLM, no solution generation, no
automatic correction.

## 2. Architecture

Browser-primary, per PLAN.md §3. A single Playwright browser context is the only
thing that talks to CodeRun. There is no separate HTTP client and no cookie copying.

```
cmd/coderun-agent/          CLI entry point (cobra)

internal/
  coderun/
    client.go               CodeRunClient interface
    models.go               domain types
    playwright/
      browser.go            context lifecycle, persistent profile, challenge detection
      auth.go               login + session validation
      selections.go         selection discovery
      problems.go           problem list + problem detail
      submissions.go        submit, poll, verdict detail
    extract/
      statement.go          html -> Problem
      katex.go              KaTeX -> TeX normalisation
      listing.go            html -> []ProblemSummary, []Selection
      status.go             class token -> Status
  storage/
    sqlite.go               schema, migrations, queries
    artifacts.go            write-only human-readable files
  config/
    config.go               env loading, defaults
```

### 2.1 The `extract` boundary

Every parser is a pure function with no Playwright types in its signature:

```go
func ParseProblem(html string, ref ProblemRef) (*Problem, error)
func ParseProblemList(html string) ([]ProblemSummary, *Pagination, error)
func ParseSelections(html string) ([]Selection, error)
func ParseStatus(className string) Status
```

The `playwright` package obtains HTML (`page.Content()`) and hands it to `extract`.
This is what makes PLAN.md §22 achievable: the entire parsing layer is tested against
saved HTML fixtures with no browser and no network.

Rationale: parsers are where the bugs live and where the site will change. Coupling
them to a live browser would make the most fragile code the least testable.

### 2.2 Library choice

`github.com/playwright-community/playwright-go`.

Community-maintained (not official) and it shells out to Playwright's Node driver,
requiring a one-time driver install. Accepted because it is the only Go binding
providing all three primitives this design depends on:

- `GetByTestId` — every stable hook found in recon is a `data-testid`.
- `SetInputFiles` — the submission path.
- `APIRequestContext` (`page.Request()`) — sharing the browser's cookie jar to read
  the verdict JSON without a second HTTP stack.

`go-rod` and `chromedp` are pure-Go but would require hand-rolling all three.

## 3. Domain model

### 3.1 Identity

Recon found four identifiers where PLAN.md §7 modelled one.

```go
type ProblemRef struct {
    SelectionSlug string // "2025-summer-common"   from URL, stable
    ProblemSlug   string // "bridge-to-the-palace" from URL, stable
    ContextID     int    // 1838                   discovered, required by submission API
}
```

- `(SelectionSlug, ProblemSlug)` is the **primary key**. URL-derived, stable, and
  safe as a filename.
- `ContextID` is **discovered, not given**. It appears only in the `solution-template`
  request the problem page issues. It is cached in SQLite once learned, and
  re-discovered if a submission call rejects it.
- `globalId` (UUID-shaped) identifies a submission. Submissions also carry a numeric
  `id`; the API is keyed by `globalId`, so that is what we store and use.

Slug and title are independent: `2025-summer-common` is titled "CodeRun Boost
Challenge". Never derive one from the other.

### 3.2 Types

```go
type Selection struct {
    Slug, Title, Group string
    ProblemCount       int
}

type ProblemSummary struct {
    Ref        ProblemRef
    Number     int
    Title      string
    Difficulty Difficulty
    Status     Status
    URL        string
}

type Problem struct {
    Ref          ProblemRef
    Number       int
    Title        string
    Difficulty   Difficulty
    Statement    string            // markdown, math as $tex$
    InputFormat  string
    OutputFormat string
    Constraints  string
    Notes        string
    Sections     map[string]string // unrecognised <h2> sections, verbatim
    Examples     []Example
    Languages    []Compiler
    URL          string
    FetchedAt    time.Time
}

type Example struct{ Input, Output string }

type Compiler struct {
    Slug    string // "python_make"
    Title   string // "Python"
    Version string // "3.12.3"
}
```

`Difficulty` is an enum parsed from Russian (`Средняя` → `Medium`) that **retains the
raw string**. Only one value was observed; an unrecognised value maps to
`DifficultyUnknown` and preserves the original rather than failing.

`Status` has exactly three values, parsed from the icon's class token
`ProblemStatus_type_<token>__<hash>`:

| Token | Status |
|---|---|
| `not_solved` | `StatusNotSolved` |
| `wrong` | `StatusWrong` |
| `solved` | `StatusSolved` |

Parse the class token, never the localised `aria-label`.

### 3.3 Interface

PLAN.md §3's interface, adjusted for the identifiers recon revealed:

```go
type CodeRunClient interface {
    ListSelections(ctx context.Context, group string) ([]Selection, error)
    ListProblems(ctx context.Context, selectionSlug string) ([]ProblemSummary, error)
    GetProblem(ctx context.Context, ref ProblemRef) (*Problem, error)
    ListCompilers(ctx context.Context, ref ProblemRef) ([]Compiler, error)
    GetTemplate(ctx context.Context, ref ProblemRef, compilerSlug string) (string, error)
    Submit(ctx context.Context, ref ProblemRef, compilerSlug string, source []byte) (*Submission, error)
    GetSubmission(ctx context.Context, globalID string) (*Submission, error)
}
```

`GetSelection` from PLAN.md §3 is dropped: a selection carries no data beyond what
`ListSelections` and `ListProblems` already return.

## 4. Authentication

Per PLAN.md §4 Option A. No password ever enters the program.

- `LaunchPersistentContext(BROWSER_PROFILE_DIR)`, default `.browser/`.
- `coderun-agent auth login` always runs headed, regardless of `HEADLESS`, because it
  is inherently interactive. It navigates to the Yandex passport URL and waits for the
  user to complete login, polling for the logged-in marker with a generous timeout.
- Login detection: `[data-testid="log-in"]` **absent** on a CodeRun page.
- `coderun-agent auth status` reports whether the persisted session is still valid.
- PLAN.md §4 Option B (`YANDEX_EMAIL`/`YANDEX_PASSWORD`) is **not implemented**. It was
  conditional in the plan ("if automated login is actually necessary"); persistent
  profile works, so the credential path is unnecessary risk. `.env.example` documents
  only non-secret settings.
- CAPTCHA, 2FA and suspicious-login challenges are surfaced to the user to complete
  manually. Never bypassed, never automated.

Known risk, flagged not solved: a session established headed may be re-challenged when
replayed headless, because the fingerprint changes. `auth status` exists to detect this
cheaply before a long run.

## 5. Crawling

### 5.1 Selections

`GET /selections?group=<group>`, default group `coderun-seasons`.
Parse `a[href^="/selections/"]`; slug is the last path segment, title is the link text.

Groups observed: `coderun-seasons`, `favourites`, `personal`, `quickstart`,
`yainterview`, `thematic`.

### 5.2 Problems (paginated — not in PLAN.md)

Rows are `[data-testid="problem-list-item"]`. Per row:

- link `a[href*="/problems/"]` → `ProblemSlug` (strip the `?filters=` query)
- text `"<number>. <title>"` → `Number`, `Title`
- `span[role="graphics-symbol"]` class token → `Status`
- difficulty label within the row → `Difficulty`

Pagination state lives in the `?filters=` query parameter, a **double-URL-encoded**
JSON object:

```json
{"difficulty":[],"search":"","sort":null,"status":[],
 "currentPage":1,"pageSize":20,"tag":[],"language":[],"groups":[]}
```

Strategy: request `currentPage: 1` and read the pager to learn the page count, then
iterate pages, honouring `REQUEST_DELAY` between navigations. Do **not** raise
`pageSize` beyond the observed 20 — an unvalidated parameter against an undocumented
endpoint is exactly the kind of thing that gets an account flagged.

Encoding this blob is done by a single tested helper; it is a documented fragile point.

### 5.3 Problem detail

Navigate to `/selections/<sel>/problems/<slug>`, then extract from `page.Content()`.

`ContextID` is captured by registering a response listener on
`**/solution-template*` before navigation and reading `problemContextId` from the
request URL. It is persisted immediately.

## 6. Statement extraction

`innerText` is unusable: KaTeX emits both MathML and styled HTML, so every formula is
duplicated (`1 ≤ N ≤ 1 0 5 1≤N≤10 5`). Feeding that to an LLM later would silently
corrupt constraints.

Algorithm, operating on parsed HTML:

1. Locate the description container via the `[data-testid="problem-title"]` H1's
   ancestor. Do not select on the hashed `Description_description__*` class.
2. Replace every `.katex` subtree with its
   `<annotation encoding="application/x-tex">` text, wrapped in `$…$`.
3. Split on `<h2>` boundaries. Map known Russian headings:
   `Формат ввода` → `InputFormat`, `Формат вывода` → `OutputFormat`,
   `Ограничения` → `Constraints`, `Примечание` → `Notes`.
   Content before the first `<h2>` is `Statement`.
4. Any unrecognised `<h2>` is preserved verbatim in `Sections`. The heading set is an
   assumption based on one observed problem; dropping unknown content would lose
   problem data silently.
5. Examples come from `[data-testid="code-snippet"]` as ordered Ввод/Вывод pairs,
   stripping the leading label line.

Implemented with `goquery` over the HTML string. Pure, fixture-testable.

## 7. Languages and templates

- The compiler list is **scraped, never hardcoded**, from
  `ul[role="listbox"] [role="option"]` where the option's `id` **is** the
  `compilerSlug`. Slugs are not derivable from names — JavaScript is `nodejs_20_make`.
- Cached per session in memory and persisted to SQLite.
- Language selection uses the URL parameter `?compiler=<slug>` rather than clicking the
  dropdown. Fewer interactions, fewer failure modes, and it is idempotent.
- The solution template is always fetched per `(problem, compilerSlug)` and never
  synthesised: the entry point differs per language (`solution` in Python, `solve` in
  Dart).

## 8. Submission

CodeRun is a **function-signature judge, not stdin/stdout**. The submitted file must
define the entry point named by that language's template.

Flow:

```
navigate /selections/<sel>/problems/<slug>?compiler=<slug>
register response listener on **/api/submission/submit
click   [data-testid="file-attach"]
setInputFiles [data-testid="file-attach-modal"] input[type=file]
click   [data-testid="confirm"]
read    globalId from the captured response
```

The upload modal states: one file per submission, **max 256 KB**. Enforce that limit
client-side with a clear error before uploading.

This path never touches Monaco. `window.monaco` is undefined so `setValue()` is
unavailable, and typing risks auto-indent corrupting whitespace-significant languages.
File upload sidesteps both. `.monaco-editor textarea` mirrors the buffer and is used
only as an optional read-back assertion.

Capturing `globalId`:

- **Primary**: the `**/api/submission/submit` response body, `result.globalId`.
  Exact, no race, listener registered before the click.
- **Fallback**: `GET /api/submission/latest?compilerSlug=&problemSlug=`, accepted only
  if its `submitAt` is after the recorded click time. This endpoint returns *a* latest
  submission, not necessarily ours, so it is a fallback only.

If neither yields a `globalId`, the submission is recorded as `SubmitUnconfirmed` with
the click timestamp. It is **not** retried — a blind resubmit could double-submit.

## 9. Polling and verdicts

Poll `page.Request().Get("/api/submission/<globalId>")` every `POLL_INTERVAL`
(default 2s) until `status == "FINISHED"`, bounded by `SUBMISSION_TIMEOUT`
(default 120s). This satisfies PLAN.md §11's "observe state, do not re-refresh".

Response fields consumed:

```
verdict, status, submitAt, compiler{slug,title,version}
maxMemoryUsageBytes, maxTimeUsageMillis
firstFailedTestNumber, compileLog
openTests{totalTests, tests[{testNumber, verdict, isSample,
          usedTimeMillis, usedMemoryBytes, input, output, answer}]}
hiddenTests{totalTests}
runtimeLimits{timeLimitMillis, memoryLimitBytes}
```

**Verdicts are treated as an open set.** Only `WRONG_ANSWER` has been observed.

```go
func IsAccepted(verdict string) bool {
    return verdict == "OK" || verdict == "ACCEPTED"
}
```

Everything else is not-accepted, stored verbatim, and logged at WARN when unrecognised.
No closed `switch`. An unlisted verdict misclassified as success is the worst available
failure mode, so the default must be "not accepted".

On a non-accepted `FINISHED` verdict, immediately fetch the failing open test's
`input`, `output` and `answer` from their presigned S3 links and store them. They
expire after 12 hours (`X-Amz-Expires=43200`) and are the raw material the future
correction loop depends on. Guarded by a size cap (default 256 KB per artifact) so a
pathological test cannot exhaust memory. Fetched via `page.Request()`.

Presigned URLs are **never persisted or logged** — they are bearer credentials.

## 10. Storage

PLAN.md §8/§15 specify JSON files while §20 specifies SQLite, both tracking problems
and submissions. Two writers over one truth guarantees drift. Resolution:

**SQLite is authoritative.** It is the only thing read back for logic, and it is what
makes PLAN.md §20's resume-after-restart work.

Tables: `selections`, `problems`, `compilers`, `attempts`, `submissions`, `test_results`.
Schema versioned with forward-only migrations.

**Files are write-only artifacts for humans**, per PLAN.md §15:

```
solutions/<selection>/<problem>/attempt-01.py
solutions/<selection>/<problem>/attempt-01.json
```

Never read back by the program. `data/problems/*.json` becomes an explicit `export`
command rather than a live cache.

Invariant: *if the program reads it, it is in the database.*

## 11. CLI

Cobra. PLAN.md §17, plus the phase-6 command:

```
coderun-agent auth login                 headed browser, manual login
coderun-agent auth status                is the session valid
coderun-agent selections [--group G]
coderun-agent problems <selection>
coderun-agent problem <selection> <slug> [--refresh]
coderun-agent submit <selection> <slug> --file F [--lang python]
coderun-agent status
coderun-agent export [--out data/]
```

`solve`, `solve --all` and `solution` are registered but return
"not implemented in this milestone" — they belong to the LLM spec.

`--lang` accepts a friendly name (`python`) resolved against the scraped compiler map;
an unknown name lists the available options rather than guessing.

## 12. Safety, rate limiting, secrets

Per PLAN.md §18:

- One browser context. `MAX_CONCURRENT_JOBS=1`. Sequential only.
- `REQUEST_DELAY` (default 1s) between navigations.
- **Challenge detector**: a CAPTCHA, a redirect to `passport.yandex.ru` mid-run, or a
  403 causes the run to **abort and report**. No retry, no backoff, no workaround.
  This is a stop condition, not a recoverable error.
- No anti-bot evasion of any kind: no UA spoofing, no stealth plugins, no proxy
  rotation.

Secrets:

- `.gitignore`: `.browser/`, `data/`, `solutions/`, `.env`, `*.db`.
- Logging redacts cookies, session tokens and `x-csrf-token`.
- Presigned S3 URLs log as `<presigned>`.
- `.env.example` contains only non-secret configuration.

Structured logging via `log/slog`, per PLAN.md §21.

## 13. Configuration

```env
HEADLESS=false
BROWSER_PROFILE_DIR=.browser
REQUEST_DELAY=1s
POLL_INTERVAL=2s
SUBMISSION_TIMEOUT=120s
MAX_CONCURRENT_JOBS=1
DB_PATH=data/coderun.db
MAX_SOURCE_BYTES=262144
MAX_ARTIFACT_BYTES=262144
```

`auth login` ignores `HEADLESS` and always runs headed.

## 14. Testing

Per PLAN.md §22, scoped to this milestone.

**Offline, default suite** — no browser, no network:

- `extract`: golden-file tests over saved real HTML for selections, problem lists,
  problem detail, KaTeX normalisation, difficulty, status tokens.
  Fixtures captured during recon and refreshed deliberately.
- Verdict logic: table-driven over recorded JSON, **including an unknown-verdict case**
  asserting it is treated as not-accepted.
- `?filters=` encode/decode round-trip.
- Storage: migrations, resume-after-restart, attempt numbering.

**Integration**, behind `//go:build integration`, never in the default suite:

- `auth status` against a live session.
- Crawl one selection and one problem (needs no auth).
- One submission end-to-end. Run deliberately, never in CI.

PLAN.md §22 also lists "LLM response parsing" and "retry logic"; both belong to the
LLM spec.

## 15. Departures from PLAN.md

| PLAN.md | Change | Why |
|---|---|---|
| §3 interface | Added `ListCompilers`, `GetTemplate`; dropped `GetSelection`; `ProblemRef` replaces `problemID` | Recon found four identifiers, and templates/compilers are required for submission |
| §4 Option B | Not implemented | Conditional in the plan; persistent profile suffices, so credentials are avoidable risk |
| §6 | Pagination added | Lists are paginated, 20/page |
| §8 vs §20 | SQLite authoritative, files write-only | The two contradicted each other |
| §9 | Three statuses, not four | Only `not_solved`/`wrong`/`solved` exist |
| §10 | File upload instead of editor manipulation | `window.monaco` is undefined; upload bypasses it entirely |
| §11 | Read verdict JSON via `page.Request()` | Far richer than DOM scraping, same browser session |
| §13/§16 | Deferred to LLM spec, but noted as function-signature | Judge is function-based, not stdin/stdout |
| §17 CLI | `problem <selection> <slug>` takes two args, not one `<id>`; `auth status` and `export` added | A problem is identified by the selection/slug pair, not a single opaque id |
| §22 | LLM/retry tests deferred | Out of milestone scope |

## 16. Known risks

1. **`compilerSlug` scraping** depends on the option `id` attribute — an undocumented
   UI internal. If it changes, language selection breaks. Mitigated by failing loudly
   with the scraped list rather than falling back to a guess.
2. **`?filters=` pagination blob** is an undocumented, double-encoded UI internal, and
   is load-bearing for problem discovery.
3. **Headless fingerprint change** may invalidate a headed session.
4. **Verdict vocabulary is incomplete.** Handled by the open-set rule.
5. **ToS.** Automating submissions to a live contest platform likely conflicts with
   CodeRun's terms, and submissions carry `isCounted: true`. The user has been informed
   and has chosen to proceed. The design does not evade rate limits or anti-bot
   measures, and stops on challenge.

## 17. Definition of done

- `auth login` establishes a session that survives a process restart.
- `selections` lists all 13 seasonal selections.
- `problems 2025-summer-common` lists all problems across both pages with correct
  numbers, titles, difficulties and statuses.
- `problem 2025-summer-common bridge-to-the-palace` yields a statement whose
  constraints read `$1 \le N \le 10^5$` and not doubled text, with both examples.
- `submit … --file solution.py --lang python` submits, polls, and records a verdict
  with per-test detail.
- Default `go test ./...` passes with no browser and no network.
