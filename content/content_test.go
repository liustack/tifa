package content

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
)

type post struct {
	Entry
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
}

type sameEntry = Entry

type twiceEntry struct {
	Entry
	sameEntry `json:"-"`
}

type deepEntry struct {
	Entry
	post
}

type ownJSON struct{ Entry }

func (ownJSON) UnmarshalJSON([]byte) error { return nil }

type ownText struct{ Entry }

func (*ownText) UnmarshalText([]byte) error { return nil }

type ownFrom struct{ Entry }

func (*ownFrom) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

func (p post) Validate() error {
	if p.Title == "bad" {
		return errors.New("title must not be bad")
	}
	return nil
}

var (
	blog  = Define[post](Source{Dir: "testblog", Languages: []string{"zh-CN", "en"}})
	notes = Define[post](Source{Dir: "testnotes"})
)

func record(id, lang, slug, group string, date time.Time, title string) content.Record {
	path := id[:strings.LastIndex(id, "/")+1] + slug
	return content.Record{ID: id, Lang: lang, Slug: slug, Path: path, Group: group, Date: date, Words: 7,
		Source: fmt.Sprintf("content/testblog/%s/%s/index.md", lang, id), Data: []byte(fmt.Sprintf(`{"title":%q,"tags":["go"]}`, title))}
}

// load gives the collections of this test their entries, and takes them away when t ends.
func load(t *testing.T, f content.File) error {
	t.Helper()
	for _, c := range content.Collections() {
		t.Cleanup(c.Save())
	}
	return content.LoadFile(f)
}

func TestEntriesTranslationsAndVisibility(t *testing.T) {
	day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	future := time.Now().Add(time.Hour)
	err := load(t, content.File{"testblog": {
		// Out of order on purpose: the collection sorts by ID, then by language.
		record("202608/free-search", "en", "free-web-search", "202608/free-search", day, "Free search"),
		record("202608/free-search", "zh-CN", "free-search", "202608/free-search", day, "免费搜索"),
		record("202611/launch", "zh-CN", "launch", "launch", future, "新版本"),
		record("202611/release-notes", "en", "release-notes", "launch", future, "Release"),
		record("about", "zh-CN", "about", "about", time.Time{}, "关于"),
		record("202607/old", "en", "old", "202607/old", day.AddDate(0, -1, 0), "Old"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range blog.All() {
		ids = append(ids, p.Lang+"/"+p.ID)
	}
	if want := []string{"en/202607/old", "zh-CN/202608/free-search", "en/202608/free-search", "zh-CN/about"}; !slices.Equal(ids, want) {
		t.Errorf("All = %q, want %q", ids, want)
	}
	en := blog.In("en")
	if len(en) != 2 || en[0].ID != "202607/old" || en[1].Slug != "free-web-search" || en[1].Path != "202608/free-web-search" {
		t.Errorf("In(en) = %+v", en)
	}
	if blog.In("fr") != nil {
		t.Error("In(fr) is not nil")
	}
	p, err := blog.Get("zh-CN", "202608/free-search")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "免费搜索" || p.Words != 7 || !p.Date.Equal(day) || !slices.Equal(p.Tags, []string{"go"}) {
		t.Errorf("Get = %+v", p)
	}
	want := []Translation{{Lang: "zh-CN", ID: "202608/free-search", Slug: "free-search", Path: "202608/free-search"}, {Lang: "en", ID: "202608/free-search", Slug: "free-web-search", Path: "202608/free-web-search"}}
	if !slices.Equal(p.Translations, want) {
		t.Errorf("Translations = %+v, want %+v", p.Translations, want)
	}
	if about, _ := blog.Get("zh-CN", "about"); len(about.Translations) != 1 || about.Translations[0].Lang != "zh-CN" {
		t.Errorf("Translations of an entry in one language = %+v", about.Translations)
	}
	for _, x := range [][2]string{{"zh-CN", "202611/launch"}, {"en", "202611/release-notes"}, {"en", "about"}, {"fr", "about"}} {
		if _, err := blog.Get(x[0], x[1]); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("Get(%q, %q) = %v, want tifa.ErrNotFound", x[0], x[1], err)
		}
	}
	// Each call returns a new slice.
	all := blog.All()
	all[0].Title = "changed"
	if blog.All()[0].Title == "changed" {
		t.Error("All shares its slice")
	}
	if !slices.Equal(blog.Languages(), []string{"zh-CN", "en"}) || notes.Languages() != nil {
		t.Errorf("Languages = %q and %q", blog.Languages(), notes.Languages())
	}
}

func TestValidateStopsTheLoad(t *testing.T) {
	err := load(t, content.File{"testblog": {record("a", "en", "a", "a", time.Time{}, "bad")}})
	if err == nil || err.Error() != "tifa: content: content/testblog/en/a/index.md: Validate: title must not be bad" {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectionWithoutLanguages(t *testing.T) {
	r := record("x", "", "x", "x", time.Time{}, "X")
	if err := load(t, content.File{"testnotes": {r}}); err != nil {
		t.Fatal(err)
	}
	if len(notes.In("")) != 1 || notes.In("en") != nil || len(notes.All()) != 1 {
		t.Errorf("In and All = %v, %v, %v", notes.In(""), notes.In("en"), notes.All())
	}
	if p, err := notes.Get("", "x"); err != nil || p.Title != "X" {
		t.Errorf("Get = %+v, %v", p, err)
	}
	defer func() {
		if r := recover(); r != `content: Get on collection testnotes, which has no languages, with lang "en": pass ""` {
			t.Errorf("panic = %v", r)
		}
	}()
	notes.Get("en", "x")
}

func TestNoDataPanicsWithAHint(t *testing.T) {
	c := content.Lookup(notes)
	t.Cleanup(c.Save())
	notes.data.Store(nil)
	defer func() {
		if r := recover(); r != "content: collection testnotes has no data: in a test call tifatest.LoadContent(t) or tifatest.SetContent" {
			t.Errorf("panic = %v", r)
		}
	}()
	notes.All()
}

func TestSetMakesTranslationsByID(t *testing.T) {
	c := content.Lookup(blog)
	t.Cleanup(c.Save())
	commit, err := c.Set([]any{
		post{Entry: Entry{ID: "a", Lang: "en", Slug: "a", Path: "a"}, Title: "A"},
		post{Entry: Entry{ID: "a", Lang: "zh-CN", Slug: "a", Path: "a"}, Title: "甲"},
		post{Entry: Entry{ID: "b", Lang: "en", Slug: "b", Path: "b", Words: 3}, Title: "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit()
	a, _ := blog.Get("en", "a")
	if len(a.Translations) != 2 || a.Translations[0].Lang != "zh-CN" {
		t.Errorf("Translations = %+v", a.Translations)
	}
	if b, _ := blog.Get("en", "b"); b.Words != 3 {
		t.Errorf("Words = %d", b.Words)
	}
	if _, err := c.Set([]any{post{Entry: Entry{ID: "a", Lang: "fr"}}}); err == nil || !strings.Contains(err.Error(), `language "fr"`) {
		t.Errorf("a language the collection lacks: %v", err)
	}
	if _, err := c.Set([]any{post{Entry: Entry{ID: "a", Lang: "en"}}, post{Entry: Entry{ID: "a", Lang: "en"}}}); err == nil {
		t.Error("the same entry twice is no error")
	}
}

func TestDefinePanicsOnWhatItSees(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func()
		want string
	}{
		{"no Entry", func() { Define[struct{ Title string }](Source{Dir: "x1"}) }, "must be a struct type that embeds content.Entry"},
		{"pointer", func() { Define[*post](Source{Dir: "x2"}) }, "must be a struct type that embeds content.Entry"},
		{"Entry twice", func() { Define[twiceEntry](Source{Dir: "x4"}) }, "content.Define: content.twiceEntry embeds content.Entry more than once: a schema embeds it once, as its own field"},
		{"UnmarshalJSON", func() { Define[ownJSON](Source{Dir: "x6"}) }, "content.Define: content.ownJSON reads its own JSON, so front matter cannot be checked against it: remove its UnmarshalJSON, UnmarshalJSONFrom or UnmarshalText method"},
		{"UnmarshalText", func() { Define[ownText](Source{Dir: "x7"}) }, "content.Define: content.ownText reads its own JSON"},
		{"UnmarshalJSONFrom", func() { Define[ownFrom](Source{Dir: "x8"}) }, "content.Define: content.ownFrom reads its own JSON"},
		{"Entry deeper", func() { Define[deepEntry](Source{Dir: "x5"}) }, "content.Define: content.deepEntry embeds content.Entry more than once: a schema embeds it once, as its own field"},
		{"bad Dir", func() { Define[post](Source{Dir: "a/b"}) }, `Dir "a/b" must be the name of one folder`},
		{"language twice", func() { Define[post](Source{Dir: "x3", Languages: []string{"en", "en"}}) }, `Languages has "en" twice`},
		{"Dir twice", func() { Define[post](Source{Dir: "testblog"}) }, "two collections read content/testblog"},
	} {
		func() {
			defer func() {
				if r, _ := recover().(string); !strings.Contains(r, c.want) {
					t.Errorf("%s: panic %q, want %q", c.name, r, c.want)
				}
			}()
			c.f()
		}()
	}
}

// Under tifa dev every entry can be seen, whatever its date, so no date is to come and no timer
// waits for one, also with a watcher.
func TestDevShowsFutureEntriesAndDrafts(t *testing.T) {
	clock := useClock(t, start)
	content.ShowFuture(true)
	t.Cleanup(func() { content.ShowFuture(false) })
	t.Cleanup(content.Watch(func() {}))
	future := start.Add(time.Hour)
	draft := record("202610/wip", "zh-CN", "wip", "202610/wip", time.Time{}, "草稿")
	draft.Draft = true
	err := load(t, content.File{"testblog": {
		record("202611/launch", "zh-CN", "launch", "launch", future, "新版本"),
		record("202611/release-notes", "en", "release-notes", "launch", future, "Release notes"),
		draft,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := blog.All(); len(got) != 3 {
		t.Fatalf("All has %d entries, want the draft and both future ones", len(got))
	}
	p, err := blog.Get("zh-CN", "202611/launch")
	if err != nil || !p.Date.Equal(future) || len(p.Translations) != 2 {
		t.Fatalf("Get = %+v, %v: want the future entry with both translations", p.Entry, err)
	}
	if d, err := blog.Get("zh-CN", "202610/wip"); err != nil || !d.Draft {
		t.Fatalf("Get draft = %+v, %v: want it with Draft", d.Entry, err)
	}
	if got := clock.pending(); len(got) != 0 {
		t.Errorf("timers %v, want none", got)
	}
}
