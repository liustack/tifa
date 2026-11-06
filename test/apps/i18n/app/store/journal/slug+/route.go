// Package slug is the page of a journal entry that has another slug in each language, or is missing
// in one. It builds the versions of the page from the translations of the entry, the way a page of a
// content collection builds them from entry.Translations.
package slug

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
)

// entries holds the translations of each entry by the slug of any of its versions, as a collection
// holds them in entry.Translations.
var entries = map[string][]content.Translation{
	"ni-hao":  {{Lang: "zh-CN", Slug: "ni-hao"}, {Lang: "en-US", Slug: "hello"}},
	"hello":   {{Lang: "zh-CN", Slug: "ni-hao"}, {Lang: "en-US", Slug: "hello"}},
	"only-zh": {{Lang: "zh-CN", Slug: "only-zh"}},
}

type Data struct {
	Slug string `json:"slug"`
}

func Load(c *tifa.Ctx) (Data, error) {
	slug := c.Param("slug")
	translations, ok := entries[slug]
	if !ok {
		return Data{}, tifa.ErrNotFound
	}
	// One version per translation, at the path its slug gives in that language. The language
	// without a translation is left out, and the entry has no page there.
	var versions []tifa.Alternate
	for _, t := range translations {
		versions = append(versions, tifa.Alternate{Language: t.Lang, Path: "/store/journal/" + t.Slug})
	}
	c.SetAlternates(versions)
	return Data{Slug: slug}, nil
}
