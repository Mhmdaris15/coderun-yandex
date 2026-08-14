package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestNormalizeKatexReplacesFormulaWithTex(t *testing.T) {
	// Structure mirrors real KaTeX output: a mathml branch carrying the
	// annotation, and an html branch carrying the visible glyphs.
	html := `<div id="root"><p>bound <span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">
       1 \le N \le 10^5
      </annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">1 ≤ N ≤ 1 0 5</span>` +
		`</span> ok</p></div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Find("#root")
	NormalizeKatex(root)

	got := strings.Join(strings.Fields(root.Text()), " ")
	want := "bound $1 \\le N \\le 10^5$ ok"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestNormalizeKatexRemovesDuplication(t *testing.T) {
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">N</annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">N</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	// Before normalisation this reads "N\nN". Exactly one N must survive.
	if got := strings.TrimSpace(root.Text()); got != "$N$" {
		t.Errorf("got %q, want %q", got, "$N$")
	}
}

func TestNormalizeKatexWithoutAnnotationDropsHiddenBranch(t *testing.T) {
	// Defensive: if KaTeX ever renders without an annotation, we must still
	// not emit the formula twice.
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics></semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">x+y</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	if got := strings.TrimSpace(root.Text()); got != "x+y" {
		t.Errorf("got %q, want %q", got, "x+y")
	}
}

func TestNormalizeKatexOnRealFixture(t *testing.T) {
	html := loadFixture(t, "problem-bridge-to-the-palace.html")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	body := doc.Find("body")
	if body.Find(".katex").Length() == 0 {
		t.Skip("fixture contains no KaTeX; re-capture if the problem changed")
	}
	NormalizeKatex(body)

	if body.Find(".katex").Length() != 0 {
		t.Error("NormalizeKatex left .katex nodes behind")
	}
	// The doubled rendering of this problem's bound is "1 0 5 1≤N≤10 5".
	// Its disappearance is the point of this function.
	if strings.Contains(body.Text(), "1≤N≤10") {
		t.Error("doubled math survived normalisation")
	}
}
