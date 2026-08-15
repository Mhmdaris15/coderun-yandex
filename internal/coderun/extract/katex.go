package extract

import (
	"html"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// NormalizeKatex replaces every KaTeX formula inside sel with its TeX source
// wrapped in dollar signs, mutating the document in place.
//
// KaTeX renders each formula twice — once as MathML for screen readers and
// once as styled HTML for sighted users — so reading text without this step
// duplicates every formula. The TeX source is recovered from the
// <annotation encoding="application/x-tex"> node in the MathML branch.
//
// Recovered text is HTML-escaped before being spliced back in. goquery's
// Text() returns decoded text, and ReplaceWithHtml re-parses its argument as
// markup, so an unescaped strict inequality like "0 < x < 10" would be read
// as an opening tag and silently destroy the constraint.
func NormalizeKatex(sel *goquery.Selection) {
	sel.Find(".katex").Each(func(_ int, k *goquery.Selection) {
		tex := strings.TrimSpace(k.Find(`annotation[encoding="application/x-tex"]`).First().Text())

		if tex == "" {
			// No annotation to recover. Drop the MathML branch and unwrap the
			// node, so the visible rendering survives exactly once and no
			// .katex element is left behind.
			k.Find(".katex-mathml").Remove()
			visible := strings.TrimSpace(k.Text())
			k.ReplaceWithHtml("<span>" + html.EscapeString(visible) + "</span>")
			return
		}
		// Collapse internal whitespace: annotations arrive pretty-printed.
		// TeX is whitespace-insensitive in maths mode, so this is safe.
		tex = strings.Join(strings.Fields(tex), " ")
		k.ReplaceWithHtml("<span>$" + html.EscapeString(tex) + "$</span>")
	})
}
