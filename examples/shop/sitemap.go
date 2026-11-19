package shop

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/journal"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// sections are the pages of the storefront that lead to the others, with their names in llms.txt.
var sections = []struct {
	path               string
	title, description storekit.Text
}{
	{"/store", storekit.Text{"山海工作室", "Shanhai Studio"}, storekit.Text{"日常器物，慢慢用旧", "Everyday things, made to wear in"}},
	{"/store/products", storekit.Text{"全部商品", "All products"}, storekit.Text{}},
	{"/store/journal", storekit.Text{"手记", "Journal"}, storekit.Text{"工作室里的日常、器物背后的故事，以及给店主们的接口文档。", "Days in the workshop, the stories behind our things, and API notes for shop owners."}},
}

// sitemap lists the pages of the storefront for search engines and language models: every product
// and every journal post, in each language, at the same path. The workbench, the sign-in page,
// the cart and the checkout are for the people who use them, not for search.
func sitemap(*tifa.Ctx) (tifa.Sitemap, error) {
	sm := tifa.Sitemap{
		Name:    "Shanhai Studio",
		Summary: "Everyday things, made to wear in: kitchenware, stationery and home goods from a small studio, with notes from the workshop.",
		Exclude: []string{"/projects", "/login", "/store/cart", "/store/checkout"},
	}
	for _, code := range tifa.Languages() {
		l, err := storekit.FindLocale(code)
		if err != nil {
			return tifa.Sitemap{}, err
		}
		for _, s := range sections {
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: code, Path: s.path, Title: s.title.In(l), Description: s.description.In(l),
			})
		}
		for _, e := range products.Entries() {
			p, err := products.GetProduct(e.Slug, l)
			if err != nil {
				return tifa.Sitemap{}, err
			}
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: code, Path: "/store/products/" + p.Slug, Title: p.Name, Description: p.Summary,
			})
		}
		for _, p := range journal.List(l) {
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: code, Path: "/store/journal/" + p.Slug, Modified: p.PublishedAt, Title: p.Title, Description: p.Excerpt,
			})
		}
	}
	return sm, nil
}
