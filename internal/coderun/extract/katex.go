package extract

import (
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
func NormalizeKatex(sel *goquery.Selection) {
	sel.Find(".katex").Each(func(_ int, k *goquery.Selection) {
		tex := strings.TrimSpace(k.Find(`annotation[encoding="application/x-tex"]`).First().Text())

		if tex == "" {
			// No annotation to recover. Drop the MathML branch so at least the
			// visible rendering is not emitted twice.
			k.Find(".katex-mathml").Remove()
			return
		}
		// Collapse internal whitespace: annotations arrive pretty-printed.
		tex = strings.Join(strings.Fields(tex), " ")
		k.ReplaceWithHtml("<span>$" + tex + "$</span>")
	})
}
