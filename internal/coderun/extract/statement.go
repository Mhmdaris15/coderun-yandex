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
