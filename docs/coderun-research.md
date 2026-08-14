# CodeRun — Site Research (Phase 1)

Observed live on 2026-08-14 against `coderun.yandex.ru`, both anonymous and
authenticated. Everything below is measured, not assumed.

One real submission was made during recon (a deliberately wrong Python solution to
`bridge-to-the-palace`) in order to capture the submit and verdict mechanisms.

## Platform

- Next.js **Pages Router**. `__NEXT_DATA__` present, `buildId` = `BXa8emZhTyCeyvpheWYWQ`.
  `buildId` changes every deploy — never hardcode it or `/_next/data/<buildId>/` URLs.
- CSS Modules, hashed class names (`SelectionCard_selection-card__link__m3s24`).
  Confirms PLAN.md §7: **never select on class names.** Use `data-testid`, roles, hrefs.
- **Rendering is mixed, and the split matters.** Corrected 2026-08-14 after a plain
  `http.Get` capture of all three page types:

  | Content | In server HTML? |
  |---|---|
  | Selection cards | **Yes** |
  | Problem list rows (`problem-list-item`) | **Yes** |
  | Problem title (`problem-title`) | **Yes** |
  | Statement body, `Формат ввода`, KaTeX, `code-snippet` examples | **No — client-rendered** |

  So listings can be read with a plain HTTP GET, but **problem statements cannot**:
  the page shell arrives server-rendered and the statement is filled in after
  hydration. Any statement extraction must read the DOM *after* JavaScript has run
  (`page.Content()`), not the raw HTTP response body.

  `pageProps.queryValues` (a dehydrated React Query cache) is empty on all three page
  types, so there is no free structured JSON in the page either.

  This was originally recorded as "everything is server-rendered", inferred from
  reading the live DOM in a browser — which necessarily showed post-JavaScript state.
  The distinction only surfaced when raw HTML was fetched without a browser.

## Authentication

- Login is `passport.yandex.ru`, standard Yandex SSO, session carried by cookies.
- Logged-out marker: `[data-testid="log-in"]` exists. Logged-in: it is absent and a
  profile menu button is present.
- **CSRF**: all mutating `/api/*` calls require header `x-csrf-token`.
  Format `<40-hex>:<unix-seconds>`, e.g. `a1cdc7c4…:1786726197`.
  Sending none returns `403 {"code":"bad-csrf"}`.
- The token is served in the page itself at
  `__NEXT_DATA__.props.pageProps.secret` — **verified**: replaying a POST with that
  value turned a 403 `bad-csrf` into a 400 field-validation error, i.e. CSRF passed.
- So the full machine recipe is: hold session cookies → GET any page → parse
  `pageProps.secret` → call the API. No browser needed after login.
- The token embeds a timestamp and should be treated as short-lived: re-read it from a
  fresh page load rather than caching it.

## Stable selectors

| Target | Selector | Notes |
|---|---|---|
| Selection card | `a[href^="/selections/"]` | slug = last path segment |
| Problem row | `[data-testid="problem-list-item"]` | |
| Problem link | `a[href*="/problems/"]` | slug + `?filters=…` |
| Problem title | `[data-testid="problem-title"]` | H1, text = `"<number>. <title>"` |
| Solve status | `span[role="graphics-symbol"]` | class token, see below |
| Examples | `[data-testid="code-snippet"]` | alternating Ввод / Вывод |
| Language listbox | `ul[role="listbox"] [role="option"]` | **`id` attr = compilerSlug** |
| Upload button | `[data-testid="file-attach"]` | disabled when logged out |
| Upload modal | `[data-testid="file-attach-modal"]` | contains `input[type=file]` |
| Modal confirm | `[data-testid="confirm"]` | performs the submit |

Statement sections are `<h2>`-headed in Russian: `Формат ввода`, `Формат вывода`,
`Примечание`, `Ограничения`, `Примеры`, with `<h3>Пример N</h3>`.

## Solve-status vocabulary (PLAN.md §9)

From the icon's class fragment `ProblemStatus_type_<token>__<hash>`:

| Token | aria-label | Meaning |
|---|---|---|
| `not_solved` | Не решалась | never attempted |
| `wrong` | Ошибка | attempted, not accepted |
| `solved` | Решена | accepted |

**Three states, not the four PLAN.md §9 assumed.** No `IN_PROGRESS` observed.
Parse the `_type_<token>` fragment, not the localized aria-label.

## Pagination (not in PLAN.md)

Problem lists are paginated — `2025-summer-common` showed 20 rows over 2 pages.
Page state lives in `?filters=`, a **double-URL-encoded** JSON blob:

```json
{"difficulty":[],"search":"","sort":null,"status":[],
 "currentPage":1,"pageSize":20,"tag":[],"language":[],"groups":[]}
```

The crawler must iterate pages (or raise `pageSize`). PLAN.md §6 assumed a flat list.

## Identifiers

Three distinct keys, only one of which PLAN.md modelled:

- `selectionSlug` — e.g. `2025-summer-common` (URL).
- `problemSlug` — e.g. `bridge-to-the-palace` (URL).
- `problemContextId` — numeric, e.g. `1838`. Identifies the problem *within a selection
  context*; required by the submission-list API. Found in the `solution-template`
  request the page issues.
- `globalId` — UUID-shaped submission id, e.g. `1001006a-7f48-105c-b62a-db256d869399`.
  Submissions also carry a numeric `id` (`2338530`); the API is keyed by `globalId`.

Slug ⇄ title is **not** derivable: `2025-summer-common` is titled
"CodeRun Boost Challenge" while `2026-summer-common` is "CodeRun Summer Challenge".

## Internal HTTP API

Envelope is uniformly `{"result": …, "error": …}`. Guessed routes return the 404 HTML
shell, so the surface is small and purpose-built — only routes below are known to exist.

| Method | Route | Purpose |
|---|---|---|
| POST | `/api/navigation/info` | session/nav info (CSRF required) |
| GET | `/api/attention` | banners |
| GET | `/api/notifications?scope=problem&slug=<slug>` | notifications |
| GET | `/api/problem/<slug>/statistics/analysis-count` | stats |
| GET | `/api/problem/<slug>/solution-template?compilerSlug=<c>&problemContextId=<n>` | starter code |
| GET | `/api/submission/latest?compilerSlug=<c>&problemSlug=<s>` | last submission for that language |
| GET | `/api/submission?problemContextId=<n>` | submission list (polling) |
| GET | `/api/submission/<globalId>` | **full submission detail** |
| POST | `/api/submission/submit` | submit (multipart, CSRF required) |
| GET | `/api/ai/request/quota`, `/api/ai/request/type?problemSlug=<s>` | CodeRun's own built-in AI |

Selection lists, problem lists and statements are **not** in this API — they come from
SSR HTML.

Note: CodeRun ships its own AI assistant (`EXPLAIN_EXAMPLES`, `SOLUTION_HELP`,
`GENERATE_TESTS`) behind a quota. Out of scope, but relevant to ToS posture.

## Languages / compiler slugs

From `ul[role="listbox"] [role="option"]`, where the option's `id` **is** the slug:

| Slug | Language |
|---|---|
| `c_make` | C 14.1.0 |
| `csharp_make` | C# 8.0.105 |
| `cpp_make` | C++ 14.1.0 |
| `dart_make` | Dart 3.7.2 (default) |
| `go_make` | Go 1.23.0 |
| `java_make` | Java 21 Temurin |
| `nodejs_20_make` | JavaScript 20.14.0 |
| `kotlin_make` | Kotlin 1.9.21 (JRE 21) |
| `pascal_make` | Pascal 3.11.0 |
| `python_make` | Python 3.12.3 |
| `rust_make` | Rust 1.80.1 |
| `swift_make` | Swift 5.10.1 |

Slugs are **not** derivable from names — note `nodejs_20_make`, not `javascript_make`.
Treat this table as a runtime-scraped map, not a constant.

Language can be selected by URL: `?compiler=python_make`. No clicking required.

## Finding 1 — Function-signature judge, not stdin/stdout

`GET /api/problem/bridge-to-the-palace/solution-template?compilerSlug=python_make&problemContextId=1838`

```json
{"result":{"content":"def solution(n, a):\n    # your code\n","sources":{}},"error":null}
```

The same problem in Dart returns `int solve(int n, List<int> a) { … }`.
Statement text agrees: *"Первый аргумент функции N"*, *"Функция должна…"*.

**The entry-point name differs per language** (`solution` in Python, `solve` in Dart),
so the template must always be fetched — never synthesised.

Consequences for PLAN.md:
- §13 (prompt) must include the fetched template and require that exact signature,
  not a `main()` reading stdin.
- §16 (local sample tests) cannot pipe stdin. It needs a per-language harness that
  imports the function and calls it with parsed arguments.
- Whether any problems are stdin-style is still unknown; treat template shape as
  authoritative per problem.

## Finding 2 — Submission is a plain multipart POST

`POST /api/submission/submit`, `content-type: multipart/form-data`, `x-csrf-token` required.

Response:

```json
{"result":{"id":2338530,
           "globalId":"1001006a-7f48-105c-b62a-db256d869399",
           "status":"PENDING",
           "source":[{"key":"coding/solutions/probe_solution.py-…",
                      "bucket":"contest-hidden","contentType":"text/x-python",
                      "size":65,"fileName":"probe_solution.py"}]},
 "error":null}
```

The UI path that produces it: `[data-testid="file-attach"]` → modal → attach file →
`[data-testid="confirm"]`. Modal states: one file per submission, **max 256 KB**, no
`accept` filter on the input.

**This bypasses Monaco entirely.** Driving it needs only `setInputFiles` + one click.

The exact multipart field names were not captured (the recorded body was empty). They
are only needed for a pure-HTTP submitter; the Playwright upload path does not require
them.

## Finding 3 — Verdict detail is rich

`GET /api/submission/<globalId>` returns everything the §14 correction loop needs:

```json
{"globalId":"…","verdict":"WRONG_ANSWER","status":"FINISHED",
 "submitAt":"2026-08-14T16:53:35.989206Z",
 "compiler":{"slug":"python_make","title":"Python","version":"3.12.3","highlight":"python"},
 "maxMemoryUsageBytes":3620864,"maxTimeUsageMillis":23,
 "firstFailedTestNumber":1,"compileLog":null,
 "source":{"link":"<presigned s3>","size":65,"fileName":"source-code"},
 "openTests":{"totalTests":2,"tests":[
    {"testNumber":1,"usedTimeMillis":23,"usedMemoryBytes":3620864,
     "isSample":true,"verdict":"WRONG_ANSWER",
     "input":{"link":"<presigned s3>","size":13},
     "output":{"link":"<presigned s3>","size":2},
     "answer":{"link":"<presigned s3>","size":2},
     "error":null}]},
 "hiddenTests":{"totalTests":83,"tests":[]},
 "runtimeLimits":{"timeLimitMillis":2000,"memoryLimitBytes":268435456},
 "isExpired":false}
```

Crucially, for each **open (sample) test** it exposes presigned S3 links to `input`,
`output` (what the program actually printed) and `answer` (expected). That is
first-class failure feedback for the LLM — far better than PLAN.md §11/§14 assumed,
which planned to scrape verdict text from the DOM.

Hidden tests report only a count (83 here). `runtimeLimits` gives the real TL/ML
(2000 ms / 256 MB), which should be stated in the generation prompt.

Presigned links carry `X-Amz-Expires=43200` (12 h) — fetch promptly, never persist.

### Status / verdict fields

- `status`: `PENDING` → `FINISHED` (poll until `FINISHED`).
- `verdict`: `WRONG_ANSWER` observed. Full vocabulary not yet enumerated; expect at
  least `OK`/`ACCEPTED`, `TIME_LIMIT_EXCEEDED`, `MEMORY_LIMIT_EXCEEDED`,
  `RUNTIME_ERROR`, `COMPILATION_ERROR`. **Treat unknown verdicts as non-accepted and
  log them** rather than switching on an assumed closed set.
- `isCounted: true` on the list endpoint — submissions count toward scoring.

## Finding 4 — KaTeX doubles all math in `innerText`

Formulas render as KaTeX, which emits MathML *and* styled HTML, so `innerText` yields:

> `1 ≤ N ≤ 1 0 5 1≤N≤10 5`

Each `.katex` node carries `<annotation encoding="application/x-tex">` with clean TeX.
Extraction must replace each `.katex` subtree with its annotation (e.g. `$…$`) before
taking text. Feeding doubled math to an LLM is a silent correctness hazard.

## Finding 5 — Monaco is present but unnecessary

`.monaco-editor` exists; `window.monaco` is **undefined**, so
`monaco.editor.getEditors()[0].setValue()` is unavailable. Typing risks auto-indent
corrupting Python. Given Finding 2, none of this matters — **use file upload.**

Useful detail: Monaco's hidden `textarea` *does* mirror the full buffer, so
`.monaco-editor textarea` is a reliable **read-back** path for verification.

## Access summary

| Capability | Auth needed? |
|---|---|
| List selections, list problems, read statements + examples | **No** |
| Solution template | No |
| Solve status, submit, verdicts | Yes |

Crawling (PLAN.md phases 3–5) can therefore be built and tested with zero auth risk.

## Sample data

- `?group=coderun-seasons` → 13 selections. Other groups: `favourites`, `personal` (7),
  `quickstart` (2), `yainterview` (6), `thematic` (7); subgroups e.g. `&subgroup=2025-summer`.
- `bridge-to-the-palace`: number 2, difficulty `Средняя`, 2 samples, 83 hidden tests,
  `problemContextId=1838`, TL 2000 ms, ML 256 MB.

## Still open

1. Full `verdict` enum (only `WRONG_ANSWER` seen).
2. Multipart field names for `POST /api/submission/submit`.
3. Whether an `IN_PROGRESS`-style status token exists.
4. Rate-limit / anti-bot behaviour under repeated submissions — untested by design.
5. Whether any problems are stdin-style rather than function-style.
