package content

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Body is where the body of one entry is: the HTML tifa build made of its Markdown, and the
// Markdown itself with the URLs the HTML has.
type Body struct {
	Collection string
	Language   string
	ID         string
	// Dist holds the files of the build, nil in tests, which have none.
	Dist fs.FS
	// File is the HTML of the body in Dist, see BodyFile. Its Markdown is next to it, see
	// MarkdownFile.
	File     string
	Headings []Heading
}

// Name names the entry in messages, such as blog en/202608/free-search.
func (b *Body) Name() string { return b.Collection + " " + path.Join(b.Language, b.ID) }

// Markdown returns the Markdown of the body as tifa build wrote it, with the URLs of the HTML.
// Without a build, as in tests, it returns a placeholder that names the entry.
func (b *Body) Markdown() (string, error) {
	if b.Dist == nil {
		return "tifatest: Markdown of " + b.Name() + "\n", nil
	}
	data, err := fs.ReadFile(b.Dist, MarkdownFile(b.File))
	if err != nil {
		return "", fmt.Errorf("the Markdown of %s: %w", b.Name(), err)
	}
	return string(data), nil
}

// MarkdownFile is where the Markdown of the body whose HTML is in file goes: the same name with
// .md, such as content/blog/en/a.md for content/blog/en/a.html.
func MarkdownFile(file string) string { return strings.TrimSuffix(file, ".html") + ".md" }

// Handle gives an entry its Body. content.Entry embeds it, so the value of every schema type is
// an Entry of this package, which tifa.SitemapPage.Content takes, while the package content
// shows no method for it.
type Handle struct{ body *Body }

// NewHandle returns the Handle of b.
func NewHandle(b *Body) Handle { return Handle{body: b} }

func (h Handle) contentBody() *Body { return h.body }

// Entry is an entry of a content collection: a value of a type that embeds content.Entry.
type Entry interface{ contentBody() *Body }

// BodyOf returns the Body of e, nil for an entry that no collection holds, such as one made in
// place.
func BodyOf(e Entry) *Body { return e.contentBody() }
