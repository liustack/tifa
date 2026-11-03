package contentbad

import (
	"time"

	"github.com/liustack/tifa/content"
)

const dir = "blog"

var (
	Blog      = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs      = content.Define[Doc](content.Source{Dir: "blog"})
	FromVar   = content.Define[Doc](src)
	ConstDir  = content.Define[Doc](content.Source{Dir: dir})
	Nested    = content.Define[Doc](content.Source{Dir: "blog/posts"})
	Missing   = content.Define[Doc](content.Source{Dir: "nothere"})
	LangVar   = content.Define[Doc](content.Source{Dir: "b", Languages: langs})
	LangTwice = content.Define[Doc](content.Source{Dir: "c", Languages: []string{"en", "en"}})
	Ptr       = content.Define[*Doc](content.Source{Dir: "d"})
	NoEntry   = content.Define[Plain](content.Source{Dir: "e"})
	BadMatch  = content.Define[Doc](content.Source{Dir: "x", Match: "a/**b"})
	NoDir     = content.Define[Doc](content.Source{Match: "*.md"})
	Inferred  = content.Define[Doc](content.Source{"x", nil, ""})
	Good      = content.Define[Doc](content.Source{Dir: "good"})
)

var src = content.Source{Dir: "x"}

var langs = []string{"en"}

func init() {
	_ = content.Define[Doc](content.Source{Dir: "x"})
}

type Post struct {
	content.Entry
	Title string    `json:"title"`
	Tags  []string  `json:"tags" yaml:"tags"`
	Date  time.Time `json:"date"`
}

type Doc struct {
	content.Entry
	Title string `json:"title"`
}

type Plain struct {
	Title string `json:"title"`
}
