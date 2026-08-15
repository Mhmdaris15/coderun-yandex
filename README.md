# coderun-agent

Automates Yandex CodeRun through a real browser. Milestone 1: crawl, extract,
and submit a manually supplied source file. No LLM yet.

## Setup

    go build ./...
    go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.0 install chromium --with-deps
    cp .env.example .env

The installer version above must match the `github.com/mxschmitt/playwright-go`
version pinned in `go.mod`, or the downloaded browser driver can mismatch the
library and fail at runtime.

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
