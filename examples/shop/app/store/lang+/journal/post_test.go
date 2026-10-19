package journal

import (
	"errors"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Every post has a Markdown body in each language, newest first in the list.
func TestEveryPostHasBothLanguages(t *testing.T) {
	for _, code := range []string{"zh-CN", "en-US"} {
		l, _ := store.FindLocale(code)
		list := List(l)
		if len(list) != 3 {
			t.Fatalf("%s: %d posts", code, len(list))
		}
		for i, s := range list {
			if i > 0 && s.PublishedAt.After(list[i-1].PublishedAt) {
				t.Errorf("%s: %s is newer than %s", code, s.Slug, list[i-1].Slug)
			}
			p, err := GetPost(s.Slug, l)
			if err != nil || !strings.Contains(p.Body, "\n## ") || p.Title != s.Title {
				t.Errorf("%s %s: %v, no sections", code, s.Slug, err)
			}
		}
	}
	zh, _ := store.FindLocale("zh-CN")
	if _, err := GetPost("nope", zh); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("unknown post: %v", err)
	}
}
