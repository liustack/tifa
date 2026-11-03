// Package article shows markup the app trusts with tifa.HTML: a note of the Layout around the
// article pages, and an article that each page below loads, whose page renders it on the server
// (read), from the whole-page cache (cached), only in the browser (plain), or not at all, since
// its component fails on the server (broken).
package article

import (
	"strings"

	"github.com/liustack/tifa"
)

// Post is an article, with its body as markup. Its body is written the way people write HTML and
// not the way a browser writes it back (<br/>, an attribute without quotes, a character reference),
// so that a page that hydrated it from a string the browser did not write would differ.
type Post struct {
	Title string      `json:"title"`
	Body  tifa.HTML   `json:"body"`
	Notes []tifa.HTML `json:"notes"`
}

// Read returns the article.
func Read() Post {
	var b strings.Builder
	b.WriteString(`<h2 id=intro>Intro</h2><p class=lead>BODY_START &amp; &#x3C;tag&#x3E; “quotes” 😀 ` + "\u2028" + `<br/>next</p>`)
	for i := range 60 {
		b.WriteString(`<p>Paragraph with <a href="#intro">a link</a>, <code>&lt;/script&gt;</code> and <em>words</em>` + strings.Repeat(" more", i%7) + `.</p>`)
	}
	b.WriteString(`<hr/><p>BODY_END</p>`)
	return Post{Title: "Article", Body: tifa.HTML(b.String()), Notes: []tifa.HTML{`<b>NOTE_ONE</b>`, `<i>NOTE_TWO</i>`}}
}
