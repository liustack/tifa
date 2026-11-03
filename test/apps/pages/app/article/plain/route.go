package plain

import (
	"example.com/pagestest/app/article"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (article.Post, error) { return article.Read(), nil }

// SSR = false renders the page only in the browser.
const SSR = false
