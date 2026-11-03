package site

import (
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs", Match: "**/*.md"})
)

type Post struct {
	content.Entry
	Title   string     `json:"title"`
	Updated *time.Time `json:"updated"`
}

type Doc struct {
	content.Entry
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
}
