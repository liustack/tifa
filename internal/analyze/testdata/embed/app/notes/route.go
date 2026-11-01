package notes

import (
	"embed"

	"example.com/embed/internal/mail"
	"github.com/liustack/tifa"
)

//go:embed posts
var posts embed.FS

//go:embed all:raw
var raw embed.FS

//go:embed schema.json data/*.txt
var data embed.FS

func Load(c *tifa.Ctx) (int, error) {
	a, _ := posts.ReadDir("posts")
	b, _ := raw.ReadDir("raw")
	d, _ := data.ReadDir("data")
	return len(a) + len(b) + len(d) + mail.Count(), nil
}
