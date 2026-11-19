package i18ntest

import (
	"context"
	"time"

	"github.com/liustack/tifa"
)

// OnStart lists the journal pages, which have a parameter, for the sitemap: hello, whose slug is
// ni-hao in zh-CN, with the Markdown of its entry, and only-zh, which has no other language and no
// entry. The page of the slug a version of hello has in the other language is not listed.
func OnStart(context.Context) error {
	tifa.SetSitemap(func(c *tifa.Ctx) (tifa.Sitemap, error) {
		hello := []tifa.Alternate{{Language: "zh-CN", Path: "/store/journal/ni-hao"}, {Language: "en-US", Path: "/store/journal/hello"}}
		// The page leave only sends its visitors elsewhere.
		sm := tifa.Sitemap{Name: "I18n Store", Summary: "A store in two languages.", Exclude: []string{"/store/leave"}}
		for _, version := range hello {
			e, err := Journal.Get(version.Language, "hello")
			if err != nil {
				return tifa.Sitemap{}, err
			}
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: version.Language, Path: version.Path, Modified: e.Date, Title: e.Title,
				Description: "The first entry of the journal.", Content: e, Alternates: hello,
			})
		}
		sm.Pages = append(sm.Pages, tifa.SitemapPage{
			Language: "zh-CN", Path: "/store/journal/only-zh", Modified: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			Alternates: []tifa.Alternate{{Language: "zh-CN", Path: "/store/journal/only-zh"}},
		})
		return sm, nil
	})
	return nil
}
