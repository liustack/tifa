package content

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
)

// loadDist gives the collections of this test the entries of a build, whose bodies are files of
// dist, and takes them away when t ends.
func loadDist(t *testing.T, dist fstest.MapFS) error {
	t.Helper()
	for _, c := range content.Collections() {
		t.Cleanup(c.Save())
	}
	return content.Load(dist)
}

const noteRecord = `{"testnotes":[{"id":"a","slug":"a","path":"a","group":"a","words":1,"source":"content/testnotes/a.md",` +
	`"file":"content/testnotes/a.html","headings":[{"depth":2,"id":"intro","text":"Intro"}],"data":{"title":"A","tags":[]}}]}`

func TestBodyReadsTheFileOfTheBuildOnEachCall(t *testing.T) {
	dist := fstest.MapFS{
		"content.json":             {Data: []byte(noteRecord)},
		"content/testnotes/a.html": {Data: []byte("<h2 id=\"intro\">Intro</h2>\n")},
	}
	if err := loadDist(t, dist); err != nil {
		t.Fatal(err)
	}
	p, err := notes.Get("", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Body(); got != tifa.HTML("<h2 id=\"intro\">Intro</h2>\n") {
		t.Errorf("Body() = %q", got)
	}
	dist["content/testnotes/a.html"] = &fstest.MapFile{Data: []byte("<p>new</p>")}
	if got := p.Body(); got != "<p>new</p>" {
		t.Errorf("Body() after the file changed = %q", got)
	}
	h := p.Headings()
	if !slices.Equal(h, []Heading{{Depth: 2, ID: "intro", Text: "Intro"}}) {
		t.Errorf("Headings() = %+v", h)
	}
	h[0].Text = "changed"
	if p.Headings()[0].Text != "Intro" {
		t.Error("Headings() returns the slice the entry holds")
	}
}

// Tests have no build: the entries of tifatest.LoadContent and SetContent have a Body that says
// where it would come from, and no headings.
func TestBodyWithoutABuildIsAPlaceholder(t *testing.T) {
	day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	if err := load(t, content.File{"testblog": {record("202608/free-search", "zh-CN", "free-search", "202608/free-search", day, "免费搜索")}}); err != nil {
		t.Fatal(err)
	}
	p, err := blog.Get("zh-CN", "202608/free-search")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Body(); got != "<p>tifatest: body of testblog zh-CN/202608/free-search</p>" {
		t.Errorf("Body() = %q", got)
	}
	if p.Headings() != nil {
		t.Errorf("Headings() = %+v", p.Headings())
	}
	c := content.Lookup(notes)
	t.Cleanup(c.Save())
	commit, err := c.Set([]any{post{Entry: Entry{ID: "b", Slug: "b", Path: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	commit()
	n, err := notes.Get("", "b")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Body(); got != "<p>tifatest: body of testnotes b</p>" {
		t.Errorf("Body() of a set entry = %q", got)
	}
}

func TestBodyOfAnEntryNoCollectionHoldsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "no collection holds") {
			t.Errorf("recover() = %v", r)
		}
	}()
	post{}.Body()
}

func TestBodyOfAMissingFilePanics(t *testing.T) {
	if err := loadDist(t, fstest.MapFS{"content.json": {Data: []byte(noteRecord)}}); err != nil {
		t.Fatal(err)
	}
	p, err := notes.Get("", "a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "content/testnotes/a.html") {
			t.Errorf("recover() = %v", r)
		}
	}()
	p.Body()
}
