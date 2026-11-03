// Package contenttest is the root of the app that tests content collections: a blog and docs in
// two languages, with drafts, an entry whose date is still to come, slugs and translation keys.
package contenttest

import (
	"fmt"
	"strings"
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs", Languages: []string{"zh-CN", "en"}})
)

const assetOrigin = "https://static.example.com/"

type Post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}

func (p Post) Validate() error {
	if p.Date.IsZero() {
		return fmt.Errorf("date: a post needs one")
	}
	if p.Cover != "" && !strings.HasPrefix(p.Cover, assetOrigin) {
		return fmt.Errorf("cover must start with %s", assetOrigin)
	}
	return nil
}

type Doc struct {
	content.Entry
	Title string `json:"title"`
	Order int    `json:"order,omitempty"` // the place in the side bar, smaller first, then by ID
}
