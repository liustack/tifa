package blog

import (
	"testing"

	"github.com/liustack/tifa/content"
	"github.com/liustack/tifa/tifatest"

	"example.com/contenttest"
)

// The blog builds its URLs from slugs, so a slug may name one post per language.
func TestSlugsAreUnique(t *testing.T) {
	tifatest.LoadContent(t)
	for _, l := range contenttest.Blog.Languages() {
		seen := map[string]string{}
		for _, p := range contenttest.Blog.In(l) {
			if other, ok := seen[p.Slug]; ok {
				t.Errorf("%s: %s and %s have the slug %s", l, other, p.ID, p.Slug)
			}
			seen[p.Slug] = p.ID
		}
	}
}

func TestBySlugFindsTheTranslation(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := BySlug("en", "free-web-search")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "202608/free-search" || len(p.Translations) != 2 || p.Translations[0].Slug != "free-search" || p.Words == 0 {
		t.Errorf("post %+v", p)
	}
	if _, err := BySlug("zh-CN", "wip"); err == nil {
		t.Error("a draft was found")
	}
}

// go test has no build, so a body is a placeholder that names the entry, and there are no headings.
func TestBodyInTests(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := BySlug("zh-CN", "free-search")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Body(); got != "<p>tifatest: body of blog zh-CN/202608/free-search</p>" {
		t.Errorf("Body() = %q", got)
	}
	if p.Headings() != nil {
		t.Errorf("Headings() = %+v", p.Headings())
	}
}

func TestTagged(t *testing.T) {
	tifatest.SetContent(t, contenttest.Blog,
		contenttest.Post{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a"}, Tags: []string{"go"}},
		contenttest.Post{Entry: content.Entry{ID: "b", Lang: "en", Slug: "b", Path: "b"}},
	)
	if got := Tagged("en", "go"); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("Tagged = %+v", got)
	}
}
