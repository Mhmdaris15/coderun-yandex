// Package extract turns CodeRun HTML into domain types.
//
// Every function here is pure: it takes strings and returns domain values.
// No Playwright type may appear in this package — that is what allows the
// whole parsing layer to be tested offline against captured fixtures.
package extract

import (
	"regexp"
	"strings"

	"coderun-agent/internal/coderun"
)

// CSS Modules hash every class name, so the only stable part of the status
// icon's class attribute is the "_type_<token>__" fragment. The localized
// aria-label ("Не решалась") is deliberately not used: it would break the
// moment the site is translated.
var statusTokenRe = regexp.MustCompile(`ProblemStatus_type_([a-z_]+)__`)

func ParseStatus(class string) coderun.Status {
	m := statusTokenRe.FindStringSubmatch(class)
	if m == nil {
		return coderun.StatusUnknown
	}
	switch m[1] {
	case "not_solved":
		return coderun.StatusNotSolved
	case "wrong":
		return coderun.StatusWrong
	case "solved":
		return coderun.StatusSolved
	default:
		return coderun.StatusUnknown
	}
}

// difficultyLabels maps observed and plausible Russian labels. Only "Средняя"
// has actually been seen; the others are best-effort and an unmatched label
// yields DifficultyUnknown with the raw text preserved.
var difficultyLabels = map[string]coderun.DifficultyLevel{
	"лёгкая":  coderun.DifficultyEasy,
	"легкая":  coderun.DifficultyEasy,
	"простая": coderun.DifficultyEasy,
	"средняя": coderun.DifficultyMedium,
	"сложная": coderun.DifficultyHard,
	"трудная": coderun.DifficultyHard,
}

func ParseDifficulty(label string) coderun.Difficulty {
	raw := strings.TrimSpace(label)
	level, ok := difficultyLabels[strings.ToLower(raw)]
	if !ok {
		level = coderun.DifficultyUnknown
	}
	return coderun.Difficulty{Level: level, Raw: raw}
}
