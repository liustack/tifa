package cached

import (
	"time"

	"example.com/pagestest/app/article"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (article.Post, error) { return article.Read(), nil }

// Cache keeps the page for every visitor for a minute.
const Cache = time.Minute
