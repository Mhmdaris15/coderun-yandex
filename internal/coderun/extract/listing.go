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
		// spaces (U+00A0) after single-letter prepositions — "В двоичном
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
