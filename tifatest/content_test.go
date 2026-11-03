package tifatest

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
