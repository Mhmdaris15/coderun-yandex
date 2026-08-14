package extract

import (
	"testing"

	"coderun-agent/internal/coderun"
)

func TestParseSelections(t *testing.T) {
	sels, err := ParseSelections(loadFixture(t, "selections.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sels) != 13 {
		t.Errorf("got %d selections, want 13 (the coderun-seasons group)", len(sels))
	}

	byslug := map[string]coderun.Selection{}
	for _, s := range sels {
		byslug[s.Slug] = s
	}

	// Slug and title are independent. This pair is the proof: a "2025" slug
	// carries a title with no year in it, and 2026 is the "Summer" one.
	if got := byslug["2025-summer-common"].Title; got != "CodeRun Boost Challenge" {
		t.Errorf("2025-summer-common title = %q, want %q", got, "CodeRun Boost Challenge")
	}
	if got := byslug["2026-summer-common"].Title; got != "CodeRun Summer Challenge" {
		t.Errorf("2026-summer-common title = %q, want %q", got, "CodeRun Summer Challenge")
	}
}

func TestParseSelectionsIgnoresNonSelectionLinks(t *testing.T) {
	sels, err := ParseSelections(loadFixture(t, "selections.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sels {
		if s.Slug == "" {
			t.Error("empty slug: a group/filter link leaked into the results")
		}
		// "?group=favourites" style links must not be treated as selections.
		if len(s.Slug) > 0 && s.Slug[0] == '?' {
			t.Errorf("query link leaked as a selection: %q", s.Slug)
		}
	}
}

func TestParseProblemList(t *testing.T) {
	probs, pages, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 20 {
		t.Errorf("got %d problems, want 20 on page 1", len(probs))
	}
	if pages != 2 {
		t.Errorf("got %d pages, want 2", pages)
	}

	first := probs[0]
	if first.Ref.ProblemSlug != "coderun-welcome" {
		t.Errorf("slug = %q, want coderun-welcome", first.Ref.ProblemSlug)
	}
	if first.Ref.SelectionSlug != "2025-summer-common" {
		t.Errorf("selection = %q", first.Ref.SelectionSlug)
	}
	if first.Number != 1 {
		t.Errorf("Number = %d, want 1", first.Number)
	}
	if first.Title != "Добро пожаловать в мир CodeRun!" {
		t.Errorf("Title = %q — the leading %q number prefix must be stripped", first.Title, "1. ")
	}
}

func TestParseProblemListReadsDifficulty(t *testing.T) {
	// Without this, a stub returning DifficultyUnknown for every row passes
	// the whole suite. Difficulty is derived by slicing row text after the
	// title, which is the most fragile extraction in this file.
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		if p.Difficulty.Raw == "" {
			t.Errorf("problem %q has an empty difficulty label — rowDifficulty is not finding it",
				p.Ref.ProblemSlug)
		}
	}
	if probs[0].Difficulty.Level == coderun.DifficultyUnknown {
		t.Errorf("problem %q difficulty %q did not map to a known level",
			probs[0].Ref.ProblemSlug, probs[0].Difficulty.Raw)
	}
}

func TestCountPagesWithoutPagerIsOne(t *testing.T) {
	// A selection short enough to fit on one page has no pager at all. That
	// must read as exactly one page, not zero.
	_, pages, err := ParseProblemList(
		`<div data-testid="problem-list-item">`+
			`<span role="graphics-symbol" class="ProblemStatus_type_solved__x"></span>`+
			`<a href="/selections/s/problems/only-one">1. Единственная Средняя</a></div>`, "s")
	if err != nil {
		t.Fatal(err)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want 1 when no pager is present", pages)
	}
}

func TestParseProblemListStripsFiltersFromSlug(t *testing.T) {
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		for _, c := range p.Ref.ProblemSlug {
			if c == '?' || c == '&' {
				t.Fatalf("slug %q still carries query parameters", p.Ref.ProblemSlug)
			}
		}
	}
}

func TestParseProblemListReadsStatus(t *testing.T) {
	probs, _, err := ParseProblemList(
		loadFixture(t, "selection-2025-summer-common.html"), "2025-summer-common")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		if p.Status == coderun.StatusUnknown {
			t.Errorf("problem %q has StatusUnknown — the status icon selector is wrong",
				p.Ref.ProblemSlug)
		}
	}
}
