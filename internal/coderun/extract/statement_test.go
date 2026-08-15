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
