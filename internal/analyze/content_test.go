package analyze

import (
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/content"
)

func TestContentCollections(t *testing.T) {
	m, err := analyzeApp(t, "content")
	if err != nil {
		t.Fatal(err)
	}
	c := m.Content
	if c == nil || len(c.Collections) != 2 {
		t.Fatalf("Content = %+v", c)
	}
	blog, docs := c.Collections[0], c.Collections[1]
	if blog.Var != "Blog" || blog.Dir != "blog" || !slices.Equal(blog.Languages, []string{"zh-CN", "en"}) || blog.Type != "example.com/content.Post" {
		t.Errorf("Blog = %+v", blog)
	}
	if docs.Var != "Docs" || docs.Dir != "docs" || docs.Match != "**/*.md" || docs.Languages != nil {
		t.Errorf("Docs = %+v", docs)
	}
	var ids []string
	for _, r := range c.File["blog"] {
		ids = append(ids, r.Lang+"/"+r.ID+"/"+r.Path)
	}
	if want := []string{"zh-CN/202608/hello/202608/hello", "en/202608/hello/202608/hello-world", "zh-CN/about/about"}; !slices.Equal(ids, want) {
		t.Errorf("blog entries = %q, want %q", ids, want)
	}
	if len(c.File["docs"]) != 1 {
		t.Errorf("docs entries = %+v", c.File["docs"])
	}
	if want := []string{"1 entry of blog has no en version: about"}; !slices.Equal(c.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", c.Warnings, want)
	}
}

func TestAppWithoutContentHasNone(t *testing.T) {
	m, err := analyzeApp(t, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if m.Content != nil {
		t.Errorf("Content = %+v", m.Content)
	}
}

// TestContentFoldersNeedACollection checks content/ of an app without content.go: every folder in
// it needs a collection all the same.
func TestContentFoldersNeedACollection(t *testing.T) {
	m, err := analyzeApp(t, "contentundeclared")
	if err == nil {
		t.Fatalf("no error, Content = %+v", m.Content)
	}
	want := "content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/"
	if got := err.Error(); got != want {
		t.Errorf("error:\n%s\nwant:\n%s", got, want)
	}
}

func TestContentDeclarationsBreakingTheRules(t *testing.T) {
	_, err := analyzeApp(t, "contentbad")
	if err == nil {
		t.Fatal("no error")
	}
	got := err.Error()
	for _, want := range []string{
		"content.go:13:2: Docs reads content/blog, which Blog reads too (content.go:12:2)",
		"content.go:14:34: the argument of content.Define must be a content.Source literal written in place, so that tifa build can read it",
		`content.go:15:54: Dir must be a string literal, such as "blog"`,
		`content.go:16:54: Dir "blog/posts" must be the name of one folder under content/: letters, digits, - and _`,
		"content.go:17:54: content/nothere does not exist: create it, or fix Dir",
		`content.go:18:70: Languages must be a []string literal of string literals, such as []string{"zh-CN", "en"}`,
		`content.go:19:70: Languages has "en" twice`,
		"content.go:20:29: *contentbad.Doc must be a struct type of this module that embeds content.Entry, not a pointer to it",
		"content.go:21:29: contentbad.Plain must be a struct type of this module that embeds content.Entry, not a pointer to it",
		`content.go:22:66: Match "a/**b" has ** inside a segment`,
		`content.go:23:34: content.Source needs Dir, the folder under content/ that holds the collection, such as Dir: "blog"`,
		`content.go:24:49: write the fields of content.Source by name, such as content.Source{Dir: "blog"}`,
		"content.go:33:6: content.Define declares a collection, which is written only as the value of a package-level var in content.go in the root of the app, such as var Blog = content.Define[Post](content.Source{Dir: \"blog\"})",
		"app/blog/route.go:13:13: content.Define declares a collection",
		"content.go:39:2: contentbad.Post.Tags has a yaml tag: front matter keys are the json names, remove the yaml tag",
		`content.go:40:2: contentbad.Post.Date has the json name "date", which content.Entry uses for the time an entry is published`,
		"content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/",
		`content/good/a.md:3:1: "tag" is not a field of contentbad.Doc.`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("errors lack %q", want)
		}
	}
	if t.Failed() {
		t.Logf("errors:\n%s", got)
	}
}

// go:embed takes only the names module.CheckFilePath allows, and the body of an entry is a file
// named by its ID.
func TestBodyFilesMustBeNamesGoEmbeds(t *testing.T) {
	f := content.File{"notes": {
		{ID: "what's-new", Source: "content/notes/what's-new.md", File: "content/notes/what's-new.html"},
		{ID: "新版本", Source: "content/notes/新版本.md", File: "content/notes/新版本.html"},
	}}
	var got []string
	for _, e := range bodyFileErrors(f) {
		got = append(got, e.Error())
	}
	want := []string{`content/notes/what's-new.md: the body of this entry goes into content/notes/what's-new.html, a name go:embed refuses: malformed file path "content/notes/what's-new.html": invalid char '\''. Rename the file`}
	if !slices.Equal(got, want) {
		t.Errorf("errors\n%q\nwant\n%q", got, want)
	}
}

// The body of a.md is the file a.html, so the body of a.html/b.md cannot go below it as into a
// folder. Both IDs are valid, so the check is on the files of the bodies.
func TestBodyFileCannotBeTheFolderOfAnother(t *testing.T) {
	f := content.File{
		"blog": {
			{ID: "a", Source: "content/blog/a.md", File: "content/blog/a.html"},
			{ID: "a.html/b", Source: "content/blog/a.html/b.md", File: "content/blog/a.html/b.html"},
			{ID: "a.html/c/d", Source: "content/blog/a.html/c/d.md", File: "content/blog/a.html/c/d.html"},
			{ID: "a.htmlx", Source: "content/blog/a.htmlx.md", File: "content/blog/a.htmlx.html"},
		},
		"docs": {
			{ID: "x", Source: "content/docs/en/x.md", File: "content/docs/en/x.html"},
			{ID: "x.html", Source: "content/docs/en/x.html/index.md", File: "content/docs/en/x.html.html"},
		},
	}
	var got []string
	for _, e := range bodyFileErrors(f) {
		got = append(got, e.Error())
	}
	want := []string{
		"content/blog/a.html/b.md: the body of this entry goes into content/blog/a.html/b.html, below content/blog/a.html, which is the body of content/blog/a.md: a file cannot also be a folder. Rename one of the two",
		"content/blog/a.html/c/d.md: the body of this entry goes into content/blog/a.html/c/d.html, below content/blog/a.html, which is the body of content/blog/a.md: a file cannot also be a folder. Rename one of the two",
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors\n%q\nwant\n%q", got, want)
	}
}
