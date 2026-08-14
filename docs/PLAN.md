# CodeRun Automation Agent — Browser-Based Implementation

I want you to build a **Go-based automation agent for Yandex CodeRun**.

The initial implementation should use **browser automation / web scraping with Playwright** rather than assuming that CodeRun has a public API.

The screenshots I provided show the actual CodeRun UI and should be treated as important reference material for the initial implementation.

---

# 1. What we are building

The ultimate goal is an automated CodeRun problem-solving system:

```text
                    Yandex CodeRun
                          │
                          ▼
                  Discover selections
                          │
                          ▼
                  Discover problems
                          │
                          ▼
                Fetch problem statement
                          │
                          ▼
                 Send to LLM
              Gemini / DeepSeek
                          │
                          ▼
                  Generate code
                          │
                          ▼
                 Local validation
                          │
                          ▼
                  Submit to CodeRun
                          │
                          ▼
                  Wait for verdict
                          │
             ┌────────────┴────────────┐
             │                         │
          ACCEPTED                FAILED
             │                         │
             ▼                         ▼
       Save solution             Send failure
                                 back to LLM
                                      │
                                      ▼
                               Generate correction
                                      │
                                      ▼
                                   Resubmit
```

The system should eventually be capable of processing a large number of CodeRun problems automatically.

However, **do not implement the entire system immediately**.

Build it incrementally.

---

# 2. CodeRun URLs

Use these real CodeRun pages as initial test targets:

```text
https://coderun.yandex.ru/selections?group=coderun-seasons

https://coderun.yandex.ru/selections/2025-summer-common

https://coderun.yandex.ru/selections/2025-summer-common/problems/bridge-to-the-palace
```

The first page contains CodeRun selections/events.

A selection page contains its problems.

A problem page contains:

* problem title
* difficulty
* problem statement
* input format
* output format
* constraints
* examples
* code editor
* programming language selector
* submit button
* submission/verdict information

The screenshots attached to this prompt show examples of these pages.

Do not hard-code the current problem list.

The crawler should discover these dynamically.

---

# 3. Important architectural decision

Do NOT assume that CodeRun has a public API.

Instead:

### Primary integration

Use:

```text
Playwright
   ↓
Chromium
   ↓
coderun.yandex.ru
```

The browser should behave like a normal user browser.

Use Playwright to:

* open pages
* authenticate
* navigate
* extract data
* interact with the problem editor
* select programming language
* submit code
* observe submission status
* collect verdicts

### Secondary investigation

While implementing Playwright, inspect browser network traffic.

Determine whether CodeRun internally uses JSON/HTTP endpoints for:

* loading selections
* loading problems
* loading problem details
* submitting solutions
* retrieving submission status

If useful internal endpoints are discovered, document them.

However:

**Do not make the entire architecture depend on undocumented endpoints.**

Keep Playwright as the fallback/primary integration layer.

Create an abstraction such as:

```go
type CodeRunClient interface {
    ListSelections(ctx context.Context) ([]Selection, error)
    GetSelection(ctx context.Context, id string) (*Selection, error)
    ListProblems(ctx context.Context, selectionID string) ([]Problem, error)
    GetProblem(ctx context.Context, problemID string) (*Problem, error)
    Submit(ctx context.Context, submission Submission) (*SubmissionResult, error)
    GetSubmissionStatus(ctx context.Context, submissionID string) (*SubmissionResult, error)
}
```

Then implement:

```text
internal/coderun/playwright/
```

first.

If later we discover stable internal HTTP endpoints, we can add:

```text
internal/coderun/http/
```

without changing the rest of the application.

---

# 4. Authentication

CodeRun requires a Yandex account.

The account I will use is my own Yandex account.

Do NOT hard-code credentials anywhere.

Do NOT put my password into:

* source code
* Git
* `.env.example`
* logs
* prompts
* README
* configuration committed to the repository

Instead, implement authentication securely.

Preferred approach:

## Option A — Persistent browser session

Use a persistent Playwright browser profile:

```text
.browser/
```

The first time the application runs:

```text
coderun-agent auth login
```

Open Chromium.

Let me manually log into Yandex.

After successful authentication, save the browser session/state locally.

Future runs reuse the authenticated browser profile.

This is preferable because it avoids storing my password entirely.

---

## Option B — Environment variables

If automated login is actually necessary, support:

```env
YANDEX_EMAIL=
YANDEX_PASSWORD=
```

But this should NOT be the default approach.

Never print these values.

Never include them in error messages.

Never commit `.env`.

Create only:

```text
.env.example
```

with empty placeholders.

If Yandex presents CAPTCHA, 2FA, suspicious-login verification, or another interactive security challenge, **stop and allow me to complete it manually**.

Do not attempt to bypass these mechanisms.

---

# 5. Browser configuration

Use Playwright with Chromium.

Support:

```text
HEADLESS=false
```

during development.

I want to be able to visually observe what the automation is doing.

For example:

```bash
coderun-agent auth login
```

should open a real browser.

Later we can support:

```env
HEADLESS=true
```

for unattended execution after the authenticated session has already been established.

Make browser configuration configurable:

```env
HEADLESS=false
BROWSER_PROFILE_DIR=.browser
```

---

# 6. First milestone: CodeRun crawler

The FIRST working milestone should NOT submit solutions.

It should simply crawl CodeRun.

Implement:

```bash
coderun-agent selections
```

Expected behavior:

```text
Opening CodeRun...

Found selections:

1. CodeRun Boost Challenge
2. CodeRun Summer Challenge
3. CodeRun Winter Challenge
...
```

Then:

```bash
coderun-agent problems 2025-summer-common
```

Expected output:

```text
CodeRun Summer Challenge

1. Кодерун и велопрокат на набережной
2. Кодерун на летнем велосипеде
3. Кодерун за кулисами фестиваля
4. Кодерун и солнечная последовательность
5. Кодерун и карта жарких точек
...
```

The crawler should extract at least:

```text
problem ID
problem slug
problem number
problem title
difficulty
URL
selection ID
selection title
```

Save the result locally.

---

# 7. Problem extraction

Implement:

```bash
coderun-agent problem <problem-id>
```

The program should open the actual problem page and extract:

```text
Title
Difficulty
Statement
Input format
Output format
Constraints
Examples
Available languages
Problem URL
```

For example, for:

```text
https://coderun.yandex.ru/selections/2025-summer-common/problems/bridge-to-the-palace
```

the resulting internal object should look approximately like:

```go
type Problem struct {
    ID           string
    Slug         string
    SelectionID  string
    Number       int
    Title        string
    Difficulty   string
    Statement    string
    InputFormat  string
    OutputFormat string
    Constraints  string
    Examples     []Example
    URL          string
}
```

Do not rely only on CSS classes that look auto-generated.

Prefer robust selectors based on:

* semantic attributes
* accessible roles
* text
* stable URL patterns
* DOM structure

Document selectors in the code.

---

# 8. Store problem data

Create:

```text
data/
├── selections/
├── problems/
└── submissions/
```

For example:

```text
data/problems/<problem-id>.json
```

Store the complete normalized problem.

This gives us a local cache and means we don't need to repeatedly scrape the same problem.

---

# 9. Detect the user's solving status

CodeRun visibly shows whether a problem has been solved.

The crawler should eventually detect statuses such as:

```text
UNSOLVED
IN_PROGRESS
ACCEPTED
FAILED
```

Do not assume the exact DOM implementation.

Inspect the actual page and determine how CodeRun exposes this information.

The goal is:

```bash
coderun-agent status
```

to eventually produce:

```text
Total:      35
Accepted:   10
Unsolved:   23
In progress: 2
```

---

# 10. Submission automation

After the crawler is working, implement submission through Playwright.

Given generated code:

```text
problem
   ↓
open problem page
   ↓
select language
   ↓
put code into editor
   ↓
click "Отправить"
   ↓
wait for submission
   ↓
extract verdict
```

The screenshot shows the CodeRun editor and the **«Отправить»** button.

Investigate the actual DOM.

Do not use arbitrary coordinate-based clicking.

Use Playwright locators whenever possible:

```text
getByRole(...)
getByText(...)
locator(...)
```

If CodeMirror/Monaco or another code editor is used, determine the correct way to set its contents.

Do not assume that simply setting the textarea value will work.

---

# 11. Submission result

After clicking submit, determine how CodeRun represents:

```text
submission ID
status
verdict
execution time
memory
compiler error
runtime error
wrong answer
accepted
```

Implement polling with a reasonable interval.

Example:

```env
POLL_INTERVAL=2s
SUBMISSION_TIMEOUT=120s
```

Do not repeatedly refresh the entire page unnecessarily.

Prefer observing the relevant UI/network state.

---

# 12. LLM integration

Once CodeRun scraping and submission work reliably, implement the LLM layer.

Create:

```go
type LLMProvider interface {
    GenerateSolution(ctx context.Context, request SolutionRequest) (*SolutionResponse, error)
}
```

Implement:

```text
Gemini
DeepSeek
```

Configuration:

```env
LLM_PROVIDER=gemini

GEMINI_API_KEY=
DEEPSEEK_API_KEY=
```

The LLM should receive the complete problem.

---

# 13. Solution-generation prompt

Send the LLM:

```text
Problem title
Problem statement
Input format
Output format
Constraints
Examples
Target programming language
```

Ask it to:

1. Understand the problem.
2. Derive the algorithm.
3. Check edge cases.
4. Determine complexity.
5. Produce a CodeRun-compatible solution.
6. Return clean source code.

Initially support:

```text
Python
Go
C++
Java
JavaScript
```

But make the language system extensible.

---

# 14. Automatic correction loop

Once basic submission works, implement:

```text
LLM
 ↓
solution
 ↓
submit
 ↓
verdict
```

If:

```text
ACCEPTED
```

stop.

If:

```text
WRONG_ANSWER
COMPILE_ERROR
RUNTIME_ERROR
```

send the failure information back to the LLM.

For example:

```text
Previous solution:

<code>

Verdict:

WRONG_ANSWER

Execution information:

<available information>

Analyze the failure carefully.

Produce a corrected solution.

Do not blindly rewrite the solution.
Identify the likely cause of failure first.
```

Then:

```text
LLM
 ↓
new solution
 ↓
submit
 ↓
verdict
```

Maximum retries should be configurable:

```env
MAX_RETRIES=5
```

---

# 15. Save every attempt

Do not overwrite previous solutions.

Use:

```text
solutions/
└── <problem-id>/
    ├── attempt-01.py
    ├── attempt-01.json
    ├── attempt-02.py
    ├── attempt-02.json
    └── final.py
```

Metadata:

```json
{
  "problem_id": "...",
  "attempt": 1,
  "language": "python",
  "provider": "gemini",
  "model": "...",
  "submission_id": "...",
  "verdict": "WRONG_ANSWER",
  "created_at": "..."
}
```

This is important because I want to later analyze how the LLM solved problems and how it corrected mistakes.

---

# 16. Local execution

Before submitting code, if possible:

```text
LLM generated code
       ↓
syntax check
       ↓
sample tests
       ↓
CodeRun submission
```

For example:

```bash
coderun-agent solve <problem-id>
```

should:

```text
[1/5] Fetching problem...
[2/5] Generating solution...
[3/5] Running sample tests...
[4/5] Submitting...
[5/5] Waiting for verdict...
```

Sample tests are only a preliminary check.

Never assume passing samples means the solution is correct.

---

# 17. CLI design

Build a clean CLI.

Required commands:

```bash
coderun-agent auth login
```

Authenticate through the browser.

```bash
coderun-agent selections
```

List selections.

```bash
coderun-agent problems <selection>
```

List problems.

```bash
coderun-agent problem <id>
```

Fetch/display one problem.

```bash
coderun-agent solve <id>
```

Generate, submit and iterate.

```bash
coderun-agent solve --all
```

Eventually solve all eligible problems.

```bash
coderun-agent status
```

Show local progress.

```bash
coderun-agent solution <id>
```

Display the final accepted solution.

---

# 18. Rate limiting

This is extremely important.

Do not create an aggressive scraper.

Implement:

```env
REQUEST_DELAY=1s
POLL_INTERVAL=2s
MAX_CONCURRENT_JOBS=1
```

Start with sequential processing.

Do not run dozens of browser sessions simultaneously.

Do not attempt to bypass CodeRun's anti-bot or rate-limiting mechanisms.

If CodeRun blocks or challenges the browser, stop and report the issue.

---

# 19. Architecture

Use clean separation:

```text
cmd/
    coderun-agent/

internal/
    coderun/
        client.go
        models.go
        playwright/
            browser.go
            auth.go
            selections.go
            problems.go
            submissions.go

    llm/
        provider.go
        gemini.go
        deepseek.go
        prompts.go

    solver/
        solver.go
        retry.go
        evaluator.go

    storage/
        storage.go
        problems.go
        submissions.go
        solutions.go

    config/
        config.go

data/
solutions/
docs/
```

The exact structure can be adjusted if your investigation suggests something better.

---

# 20. Database

Use SQLite for persistent state.

Track:

```text
selections
problems
attempts
submissions
solutions
```

This allows:

```text
application stopped
       ↓
application restarted
       ↓
resume from previous state
```

without losing progress.

---

# 21. Logging

Use structured logs.

Example:

```text
[INFO] Starting CodeRun browser
[INFO] Authentication session loaded
[INFO] Fetching selections
[INFO] Found 35 selections
[INFO] Fetching selection: 2025-summer-common
[INFO] Found 15 problems
[INFO] Opening problem: bridge-to-the-palace
[INFO] Generating solution with Gemini
[INFO] Submitting solution
[INFO] Submission ID: XXXXX
[INFO] Verdict: WRONG_ANSWER
[INFO] Generating correction, attempt 2
[INFO] Verdict: ACCEPTED
[INFO] Saving final solution
```

Never log:

```text
YANDEX_PASSWORD
API keys
session cookies
authentication tokens
```

---

# 22. Testing

Create tests for:

* problem parsing
* selection parsing
* difficulty parsing
* URL parsing
* submission state parsing
* verdict parsing
* LLM response parsing
* retry logic
* SQLite persistence

For browser automation, create a small number of integration tests that run against the actual site only when explicitly requested.

Do not make the entire unit test suite depend on CodeRun being online.

---

# 23. Development workflow

Follow this exact order.

## Phase 1

Investigate the actual CodeRun website.

Inspect:

```text
https://coderun.yandex.ru/selections?group=coderun-seasons
```

and:

```text
https://coderun.yandex.ru/selections/2025-summer-common
```

and one problem page.

Determine:

* DOM structure
* stable selectors
* navigation
* authentication behavior
* editor implementation
* submission UI
* verdict UI
* relevant network requests

Create:

```text
docs/coderun-research.md
```

with your findings.

## Phase 2

Implement authentication/session management.

```bash
coderun-agent auth login
```

I should be able to manually authenticate in the browser.

Verify that the authenticated session persists.

## Phase 3

Implement:

```bash
coderun-agent selections
```

## Phase 4

Implement:

```bash
coderun-agent problems <selection>
```

## Phase 5

Implement:

```bash
coderun-agent problem <id>
```

and save the complete problem locally.

## Phase 6

Implement submission of a **manually supplied test solution**.

For example:

```bash
coderun-agent submit <problem-id> --file solution.py
```

This phase is extremely important.

Before connecting an LLM, prove that the automation can reliably:

```text
open problem
set language
insert code
submit
wait
read verdict
```

## Phase 7

Connect Gemini.

## Phase 8

Connect DeepSeek.

## Phase 9

Implement automatic correction.

## Phase 10

Implement `solve --all`.

---

# 24. Very important: do not over-engineer before proving the browser workflow

The first objective is NOT:

> "Build the entire autonomous coding agent."

The first objective is:

> **Prove that Playwright can reliably log into CodeRun, discover a selection, open a problem, read its statement, put code into the editor, submit it, and retrieve the verdict.**

Once that works, build the LLM layer around it.

---

# 25. First task for you right now

Start by inspecting the repository and then investigate the CodeRun website.

Do NOT implement the complete application yet.

I want you to:

1. Inspect the existing project.
2. Set up a minimal Go project if necessary.
3. Install/configure Playwright.
4. Open CodeRun using Chromium.
5. Implement persistent browser-session support.
6. Let me manually log into Yandex.
7. Verify that the session persists.
8. Crawl the selections page.
9. Crawl one selection.
10. Crawl one problem.
11. Extract the problem statement and metadata.
12. Investigate the editor and submission UI.
13. Investigate network requests in parallel.
14. Document your findings in `docs/coderun-research.md`.
15. Only after these steps, propose the next implementation phase.

Do not guess how CodeRun works.

**Inspect the actual application first and build against what is really there.**
