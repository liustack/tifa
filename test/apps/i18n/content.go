// Package i18ntest is the root of the app that tests the languages of a site: a store in two
// languages with a journal whose entries link to the pages of the store.
package i18ntest

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
)

var Journal = content.Define[Entry](content.Source{Dir: "journal", Languages: tifa.Languages()})

type Entry struct {
	content.Entry
	Title string `json:"title"`
}
