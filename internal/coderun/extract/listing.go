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

		number, title := 0, strings.TrimSpace(a.Text())
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

// rowDifficulty recovers the difficulty label, which sits in the row's text
// after the title and has no dedicated test id.
func rowDifficulty(row *goquery.Selection, title string) string {
	text := strings.Join(strings.Fields(row.Text()), " ")
	if i := strings.LastIndex(text, title); i >= 0 {
		return strings.TrimSpace(text[i+len(title):])
	}
	return ""
}

// countPages reads the highest numeric label in the pager. A single-page list
// has no pager, which correctly yields 1.
func countPages(doc *goquery.Document) int {
	max := 1
	doc.Find(`nav a, nav button`).Each(func(_ int, e *goquery.Selection) {
		n, err := strconv.Atoi(strings.TrimSpace(e.Text()))
		if err == nil && n > max {
			max = n
		}
	})
	return max
}
