package tifatest

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/content"
)

type testPost struct {
	content.Entry
	Title string `json:"title"`
}

func (p testPost) Validate() error {
	if p.Title == "invalid" {
		return errors.New("title is invalid")
	}
	return nil
}

var testBlog = content.Define[testPost](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})

func TestLoadContentReadsTheSourceFiles(t *testing.T) {
	t.Chdir(filepath.Join("testdata", "contentapp", "content"))
	t.Run("load", func(t *testing.T) {
		LoadContent(t)
		var got []string
		for _, p := range testBlog.All() {
			got = append(got, fmt.Sprintf("%s/%s %q %d words, %d translations", p.Lang, p.ID, p.Title, p.Words, len(p.Translations)))
		}
		want := []string{`zh-CN/hello "你好" 4 words, 2 translations`, `en/hello "Hello" 3 words, 2 translations`}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("entries:\n%s\nwant (no draft, nothing still to come):\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
	// The collection has no data again after the test.
	defer func() {
		if r := recover(); r == nil {
			t.Error("the collection kept its entries after the test")
		}
	}()
	testBlog.All()
}

func TestLoadContentFailsOnWhatTheBuildRefuses(t *testing.T) {
	t.Chdir(filepath.Join("testdata", "badcontent"))
	msg := fatal(t, LoadContent)
	if !strings.Contains(msg, `content/blog/en/a.md:2:1: "titel" is not a field of tifatest.testPost. Did you mean "title"?`) {
		t.Errorf("message %q", msg)
	}
}

func TestLoadContentRunsValidate(t *testing.T) {
	t.Chdir(filepath.Join("testdata", "invalidcontent"))
	msg := fatal(t, LoadContent)
	if msg != "tifatest.LoadContent: tifa: content: content/blog/en/a.md: Validate: title is invalid" {
		t.Errorf("message %q", msg)
	}
}

func TestSetContent(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		SetContent(t, testBlog,
			testPost{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a", Words: 5}, Title: "A"},
			testPost{Entry: content.Entry{ID: "a", Lang: "zh-CN", Slug: "a", Path: "a"}, Title: "甲"},
			testPost{Entry: content.Entry{ID: "b", Lang: "en", Slug: "b", Path: "b"}, Title: "invalid"},
		)
		a, err := testBlog.Get("en", "a")
		if err != nil || a.Words != 5 || len(a.Translations) != 2 || a.Translations[0].Lang != "zh-CN" {
			t.Errorf("Get = %+v, %v", a, err)
		}
		if len(testBlog.In("en")) != 2 {
			t.Errorf("In(en) = %+v", testBlog.In("en"))
		}
	})
	msg := fatal(t, func(t testing.TB) {
		SetContent(t, testBlog, testPost{Entry: content.Entry{ID: "a", Lang: "fr"}})
	})
	if !strings.Contains(msg, `tifatest.SetContent: entry a has the language "fr"`) {
		t.Errorf("message %q", msg)
	}
}

func TestSetContentTime(t *testing.T) {
	t.Chdir(filepath.Join("testdata", "contentapp", "content"))
	later := time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC) // the date of en/later, a date alone
	visible := func() []string {
		var got []string
		for _, p := range testBlog.All() {
			got = append(got, p.Lang+"/"+p.ID)
		}
		return got
	}
	t.Run("after LoadContent", func(t *testing.T) {
		LoadContent(t)
		t.Run("at the date", func(t *testing.T) {
			SetContentTime(t, later)
			if got := visible(); !slices.Equal(got, []string{"zh-CN/hello", "en/hello", "en/later"}) {
				t.Errorf("at %v: %q", later, got)
			}
		})
		// The time is the real one again after the test that set it.
		if got := visible(); !slices.Equal(got, []string{"zh-CN/hello", "en/hello"}) {
			t.Errorf("after the test: %q", got)
		}
		t.Run("a second before", func(t *testing.T) {
			SetContentTime(t, later.Add(-time.Second))
			if got := visible(); !slices.Equal(got, []string{"zh-CN/hello", "en/hello"}) {
				t.Errorf("a second before %v: %q", later, got)
			}
		})
	})
	t.Run("before LoadContent", func(t *testing.T) {
		SetContentTime(t, later)
		LoadContent(t)
		if got := visible(); len(got) != 3 {
			t.Errorf("%q", got)
		}
	})
	t.Run("with SetContent", func(t *testing.T) {
		SetContent(t, testBlog, testPost{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a", Date: later}})
		if got := visible(); len(got) != 0 {
			t.Errorf("before the date: %q", got)
		}
		SetContentTime(t, later)
		if got := visible(); !slices.Equal(got, []string{"en/a"}) {
			t.Errorf("at the date: %q", got)
		}
	})
}

func TestContentRefusesParallelTests(t *testing.T) {
	for name, f := range map[string]func(testing.TB){
		"LoadContent":    LoadContent,
		"SetContent":     func(t testing.TB) { SetContent(t, testBlog) },
		"SetContentTime": func(t testing.TB) { SetContentTime(t, time.Now()) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := fatal(t, f)
			want := "tifatest." + name + ": the content of a collection is shared by the whole test binary, so a test that sets it cannot run in parallel: remove t.Parallel from this test and its parents"
			if msg != want {
				t.Errorf("message %q\nwant    %q", msg, want)
			}
		})
	}
}

// fatal runs f with a testing.TB whose Fatal stops f, and returns the message it got.
func fatal(t *testing.T, f func(testing.TB)) string {
	t.Helper()
	tb := &fatalTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(tb)
	}()
	<-done
	if tb.msg == "" {
		t.Fatal("no Fatal")
	}
	return tb.msg
}

type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatal(args ...any) {
	f.msg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
