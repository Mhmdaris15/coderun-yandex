package pwclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync"

	"github.com/mxschmitt/playwright-go"

	"coderun-agent/internal/coderun"
	"coderun-agent/internal/coderun/extract"
	"coderun-agent/internal/config"
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
