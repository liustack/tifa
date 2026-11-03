package blog

import (
	"slices"

	"github.com/liustack/tifa"

	"example.com/contenttest"
)

// Published is the posts of a language that can be seen now, newest first.
func Published(lang string) []contenttest.Post {
	list := contenttest.Blog.In(lang) // sorted by ID, drafts and posts still to come left out
	slices.SortStableFunc(list, func(a, b contenttest.Post) int { return b.Date.Compare(a.Date) })
	return list
}

func BySlug(lang, slug string) (contenttest.Post, error) {
	for _, p := range contenttest.Blog.In(lang) {
		if p.Slug == slug {
			return p, nil
		}
	}
	return contenttest.Post{}, tifa.ErrNotFound
}

func Tagged(lang, tag string) []contenttest.Post {
	return slices.DeleteFunc(Published(lang), func(p contenttest.Post) bool { return !slices.Contains(p.Tags, tag) })
}
