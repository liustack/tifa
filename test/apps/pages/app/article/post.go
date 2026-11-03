// Package article shows markup the app trusts with tifa.HTML: a note of the Layout around the
// article pages, and an article that each page below loads, whose page renders it on the server
// (read), from the whole-page cache (cached), only in the browser (plain), or not at all, since
// its component (broken) or a Layout between it and the Layout of the articles (shaky) fails on
// the server.
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

// Links returns an article of links of every kind, each with its own text: to pages of the app,
// to an anchor of itself, to other sites and schemes, and to what Go answers without a page. The
// public file /guide/notes.txt also matches the page /guide/{lang}.
func Links() Post {
	return Post{Title: "Links", Body:`<p>LINKS_START <a href="/article/read">next <em>article</em></a>, ` +
		`<a href="/article/cached#intro">cached intro</a>, <a href="/">home page</a>, <a href="#end">to the end</a>.</p>` +
		`<p><a href="https://tanstack.com/router">TanStack</a>, <a href="//example.com/x">example</a>, <a href="mailto:a@example.com">write</a>, ` +
		`<a href="/article/plain" target="_blank">new tab</a>, <a href="/guide/notes.txt" download>download notes</a>, ` +
		`<a href="/article/read" rel="external">reload read</a>, <a href="/article/read" data-own>own handler</a>.</p>` +
		`<p><a href="/guide/notes.txt">notes file</a>, <a href="/assets/missing.js">missing asset</a>, <a href="/api/ping">ping</a>, ` +
		`<a href="/nowhere/at/all">nowhere</a>, <a href="/__tifa/nothing">tifa path</a>.</p>` +
		`<p style="height:1500px">Spacer.</p><h2 id="end">End</h2><p>LINKS_END</p>`}
}
