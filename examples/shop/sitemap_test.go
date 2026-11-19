package shop_test

import (
	"io"
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

// The sitemap lists every product and journal post in both languages, and leaves out the pages
// that are not for search.
func TestSitemapListsTheStorefront(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	get := func(path string) string {
		t.Helper()
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET %s: %d %v %s", path, res.StatusCode, err, body)
		}
		return string(body)
	}
	sitemap := get("/sitemap.xml")
	for _, want := range []string{
		"<loc>https://shop.example.com/store</loc>",
		"<loc>https://shop.example.com/zh-CN/store/products/pour-over-kettle</loc>",
		"<loc>https://shop.example.com/store/journal/kettle-photos</loc><lastmod>2026-09-03T02:00:00Z</lastmod>",
	} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("the sitemap lacks %s", want)
		}
	}
	for _, gone := range []string{"/projects", "/login", "/store/cart", "/store/checkout"} {
		if strings.Contains(sitemap, gone+"<") || strings.Contains(sitemap, gone+"/") {
			t.Errorf("the sitemap has %s", gone)
		}
	}
	if llms := get("/llms.txt"); !strings.HasPrefix(llms, "# Shanhai Studio\n") || !strings.Contains(llms, "- [Journal](https://shop.example.com/store/journal): Days in the workshop, the stories behind our things, and API notes for shop owners.\n") || !strings.Contains(llms, "- [How we photograph a kettle](https://shop.example.com/store/journal/kettle-photos): ") {
		t.Errorf("llms.txt:\n%s", llms)
	}
}
