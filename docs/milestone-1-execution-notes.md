# Milestone 1 — execution notes

What was learned building milestone 1, kept because it is not recoverable from the
code or the commit messages. The design is in
[`docs/superpowers/specs/2026-08-14-coderun-agent-design.md`](superpowers/specs/2026-08-14-coderun-agent-design.md);
site facts are in [`docs/coderun-research.md`](coderun-research.md).

## Corrections to the original site research

Four assumptions in the original recon turned out to be wrong. All four came from the
same mistake: **inspecting the site through a browser after interacting with it, then
inferring the mechanism from what was on screen.** DevTools shows you the end state,
not how it was produced.

| Assumed | Actually |
|---|---|
| All pages server-rendered | Listings are; **problem statements are hydrated in.** A plain `http.Get` returns the shell without the statement, KaTeX, or examples |
| Compilers are an ARIA listbox (`role="option"`, `id` = slug) | A native `<select>` with `value` = slug. `role="option"` appears **zero** times server-side — the listbox only exists once the dropdown is opened |
| Example captions are the snippet's first line | Caption and content are sibling elements with no separator; snippet text reads `"Ввод5\n2 0 -3 3 6"`. Read the inner `<pre>` |
| Pagination uses the `?filters=` blob | The pager uses plain `?currentPage=&pageSize=&search=`. The `?filters=` blob is only *carried* by problem-detail links to restore list state, and is never decoded |

The pagination one cost the most: a fully tested `EncodeFilters` helper, byte-exact
against a real captured string, aimed at the wrong mechanism entirely. Deleted in the
final cleanup. Verifying that an implementation matches observed reality does not
verify that the right thing was observed.

**Still on the table:** the selection page's `__NEXT_DATA__` carries a structured
problem list with canonical `difficulty`/`solutionState` enums, exact `total`, and the
numeric `problemContextId` that currently requires intercepting a network request.
Deliberately deferred in favour of DOM scraping; see the "Future improvement" section
of the research doc. It would remove the locale dependence and most of the fragility
catalogued above.

## Environment fault: intermittently lossy file reads

Reads of files in this repo intermittently returned **code blocks with empty function
bodies** while the surrounding prose looked completely normal. Traced to the headroom
compression MCP. It affected at least six tasks and both implementers and reviewers.

This is dangerous precisely because it is silent and plausible: an agent shown an empty
function body and a correct prose description will reconstruct the body and believe it
transcribed faithfully. Two tasks produced subtly wrong code this way before the cause
was identified — and the first diagnosis blamed model capability, which was wrong.

**Workarounds that proved reliable**, in order of preference:
1. Small (~15 line) `sed -n 'A,Bp'` windows on disk. Large windows corrupt; small ones
   almost always come through clean.
2. `mcp__headroom__headroom_retrieve` using the compression-marker hash.
3. Reading via `base64`.

**Rule for future work:** before using code read from a file, confirm the function
bodies contain statements. Never reconstruct from prose — re-read another way, or stop.

## Deferred, with rulings

- **TOCTOU on the uploaded file.** Narrowed but not eliminated: the source is now read
  once and staged to a temp file. Fine to defer — the agent owns the artifact and there
  are no concurrent writers.
- **`Selection.ProblemCount`** is never populated and persists as 0. Recorded as a spec
  departure rather than implemented.
- **`export` command** (spec §10/§11) not implemented; `problem --json` covers the need.

## What the tests actually caught

Recorded because it shaped how the milestone was built:

- Every silent defect was found by comparing an **artifact** against ground truth — real
  captured markup, the module's own `go.mod`, the JSON actually written to disk, a live
  HTTP response. Reasoning about code found one bug (the KaTeX injection); artifacts
  found the rest.
- The most common defect class across the whole run was **tests that could not fail**:
  `err != nil` where the function always errored on that input, `Sections != nil`,
  `os.Stat` succeeding on a file whose contents were never checked. Each looked like
  coverage and verified nothing.
- The integration tests earned their cost once, decisively: pagination passed every unit
  test (they parse a captured page-1 fixture) and was broken against the live site.
- Two Critical defects were **gaps between correct tasks** — the `submissions` and
  `test_results` tables were created by one task and written by none. Per-task review
  cannot see that; the whole-branch review is what caught it.
