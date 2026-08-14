package extract

import (
	"testing"

	"coderun-agent/internal/coderun"
)

func TestParseStatus(t *testing.T) {
	cases := []struct {
		name  string
		class string
		want  coderun.Status
	}{
		{"not solved", "ProblemStatusIcon_ProblemStatus__vf4en ProblemStatusIcon_ProblemStatus_type_not_solved__kcGLp ProblemListItem_Icon__n062g", coderun.StatusNotSolved},
		{"wrong", "ProblemStatusIcon_ProblemStatus_type_wrong__abc12", coderun.StatusWrong},
		{"solved", "ProblemStatusIcon_ProblemStatus_type_solved__xy890", coderun.StatusSolved},
		{"empty", "", coderun.StatusUnknown},
		{"unrecognised token", "ProblemStatusIcon_ProblemStatus_type_in_review__q1w2e", coderun.StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseStatus(tc.class); got != tc.want {
				t.Errorf("ParseStatus() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseDifficultyKeepsRaw(t *testing.T) {
	d := ParseDifficulty("Средняя")
	if d.Level != coderun.DifficultyMedium {
		t.Errorf("Level = %v, want Medium", d.Level)
	}
	if d.Raw != "Средняя" {
		t.Errorf("Raw = %q, want the original label preserved", d.Raw)
	}
}

func TestParseDifficultyUnknownPreservesLabel(t *testing.T) {
	// Only "Средняя" was observed in the wild. An unrecognised label must not
	// be lost or guessed at — it is preserved verbatim for later inspection.
	d := ParseDifficulty("Экстремальная")
	if d.Level != coderun.DifficultyUnknown {
		t.Errorf("Level = %v, want Unknown", d.Level)
	}
	if d.Raw != "Экстремальная" {
		t.Errorf("Raw = %q, want the original label preserved", d.Raw)
	}
}
