package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads a captured page. Every extract test uses this.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("missing fixture %s: %v\n\nRun: go run ./cmd/capture-fixtures", name, err)
	}
	return string(b)
}

func TestFixturesAreServerRendered(t *testing.T) {
	cases := []struct {
		fixture string
		want    string
	}{
		{"selections.html", "CodeRun Boost Challenge"},
		{"selection-2025-summer-common.html", "problem-list-item"},
		{"problem-bridge-to-the-palace.html", "Формат ввода"},
		{"problem-bridge-to-the-palace.html", "problem-title"},
	}
	for _, tc := range cases {
		html := loadFixture(t, tc.fixture)
		if !strings.Contains(html, tc.want) {
			t.Errorf("%s does not contain %q — the page may not be server-rendered; "+
				"switch capture-fixtures to a browser-based capture", tc.fixture, tc.want)
		}
	}
}
