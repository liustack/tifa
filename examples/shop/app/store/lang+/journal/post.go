// Package journal is the shop's journal: posts written in Markdown in both languages, about the
// workshop and about the open API merchants sync their stock with. The journal list and the post
// page call it.
package journal

import (
	"embed"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Summary is a post as the list shows it.
type Summary struct {
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Excerpt     string    `json:"excerpt"`
	Author      string    `json:"author"`
	PublishedAt time.Time `json:"publishedAt"`
	Tags        []string  `json:"tags"`
	Minutes     int       `json:"minutes"` // reading time
}

// Post is the whole post. Body is Markdown, which the page renders.
type Post struct {
	Summary
	Body string `json:"body"`
}

//go:embed posts
var files embed.FS

type postRecord struct {
	slug      string
	title     store.Text
	excerpt   store.Text
	author    store.Text
	published time.Time
	tags      []store.Text
	minutes   int
}

var posts = []postRecord{
	{
		slug:      "stock-sync-api",
		title:     store.Text{"用开放接口同步库存", "Syncing stock with the open API"},
		excerpt:   store.Text{"线下门店和网店共用一份库存：从拿到令牌、拉取商品，到用 webhook 接住每一次变动。", "One stock count for the shop floor and the web shop: from a token and a product pull to a webhook for every change."},
		author:    store.Text{"陆川", "Chuan Lu"},
		published: time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC),
		tags:      []store.Text{{"开放接口", "API"}, {"库存", "Stock"}},
		minutes:   9,
	},
	{
		slug:      "theme-css",
		title:     store.Text{"用 CSS 变量给店铺换主题", "Theming your shop with CSS variables"},
		excerpt:   store.Text{"店铺主题只认十几个 CSS 变量。改颜色、圆角和字体，不用碰模板。", "A shop theme reads a dozen CSS variables. Change colours, corners and type without touching a template."},
		author:    store.Text{"叶青", "Qing Ye"},
		published: time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC),
		tags:      []store.Text{{"主题", "Themes"}, {"CSS", "CSS"}},
		minutes:   6,
	},
	{
		slug:      "kettle-photos",
		title:     store.Text{"我们怎么给手冲壶拍照", "How we photograph a kettle"},
		excerpt:   store.Text{"一盏灯、一块白板和很多耐心。商品图背后的布光、器材和后期。", "One lamp, one white card and a lot of patience: the light, the kit and the edit behind our product photos."},
		author:    store.Text{"江屿", "Yu Jiang"},
		published: time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC),
		tags:      []store.Text{{"幕后", "Behind the scenes"}},
		minutes:   5,
	},
}

// List returns every post, newest first, in the language of l.
func List(l store.Locale) []Summary {
	var out []Summary
	for _, p := range posts {
		out = append(out, p.summary(l))
	}
	return out
}

// GetPost returns the post slug in the language of l, or tifa.ErrNotFound.
func GetPost(slug string, l store.Locale) (Post, error) {
	for _, p := range posts {
		if p.slug == slug {
			body, err := files.ReadFile("posts/" + slug + "." + l.Code + ".md")
			if err != nil {
				return Post{}, err
			}
			return Post{Summary: p.summary(l), Body: string(body)}, nil
		}
	}
	return Post{}, tifa.ErrNotFound
}

func (p postRecord) summary(l store.Locale) Summary {
	s := Summary{
		Slug: p.slug, Title: p.title.In(l), Excerpt: p.excerpt.In(l), Author: p.author.In(l),
		PublishedAt: p.published, Minutes: p.minutes, Tags: []string{},
	}
	for _, t := range p.tags {
		s.Tags = append(s.Tags, t.In(l))
	}
	return s
}
