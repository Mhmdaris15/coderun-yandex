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
	//
	// The MathML branch carries real glyph text here (<mi>/<mo>), exactly as
	// KaTeX emits it. That matters: with an empty <semantics> a no-op
	// implementation would pass this test, making it useless as a guard.
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics><mrow>` +
		`<mi>x</mi><mo>+</mo><mi>y</mi></mrow></semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">x+y</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	if got := strings.TrimSpace(root.Text()); got != "x+y" {
		t.Errorf("got %q, want %q (a no-op implementation yields \"x+yx+y\")", got, "x+y")
	}
	if root.Find(".katex").Length() != 0 {
		t.Error("a .katex node survived the no-annotation path")
	}
}

func TestNormalizeKatexEscapesMarkupInTex(t *testing.T) {
	// Strict inequalities are everywhere in competitive programming. The TeX
	// source contains a literal '<', which must never be spliced into an HTML
	// string and re-parsed as a tag.
	//
	// The '<' must be followed immediately by a letter, with no space. HTML5
	// only enters tag-open state when '<' is directly followed by an ASCII
	// letter, so "0 < x" survives an unescaped splice by luck while "0<x"
	// does not. Only the no-space form discriminates a correct implementation
	// from a broken one.
	html := `<div id="root"><span class="katex">` +
		`<span class="katex-mathml"><math><semantics>` +
		`<annotation encoding="application/x-tex">0&lt;x&lt;10</annotation>` +
		`</semantics></math></span>` +
		`<span class="katex-html" aria-hidden="true">0&lt;x&lt;10</span></span></div>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	root := doc.Find("#root")
	NormalizeKatex(root)

	if got := strings.TrimSpace(root.Text()); got != "$0<x<10$" {
		t.Errorf("got %q, want %q — TeX was re-parsed as markup", got, "$0<x<10$")
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
