package e2e

import (
	"strconv"
	"strings"
	"testing"
)

// checkStorefrontPageCache checks the whole-page cache on the storefront: a page is made once and
// then answered from memory to every visitor alike, whatever cookies they send, and the
// workbench and the visitor's own data stay out of it. The query string e2e makes the URLs new
// to this run, since the checks before it opened the others.
func checkStorefrontPageCache(t *testing.T, base string) {
	t.Helper()
	age := func(h map[string][]string) int {
		t.Helper()
		n, err := strconv.Atoi(strings.Join(h["Age"], ""))
		if err != nil {
			t.Fatalf("Age %v: %v", h["Age"], err)
		}
		return n
	}

	page := base + "/store/zh-CN/journal?e2e=cache"
	status, h1, body1 := get(t, page)
	if status != 200 || h1.Get("Cache-Status") != "tifa; fwd=miss" || h1.Get("X-Tifa-Render") != "vm" {
		t.Fatalf("first request: %d %v", status, h1)
	}
	status, h2, body2 := get(t, page)
	if status != 200 || h2.Get("Cache-Status") != "tifa; hit" || h2.Get("X-Tifa-Render") != "cache" || body2 != body1 {
		t.Errorf("second request: %d %v", status, h2)
	}
	if cc := h2.Get("Cache-Control"); !strings.Contains(cc, "public") || !strings.Contains(cc, "s-maxage=600") {
		t.Errorf("Cache-Control of a kept page: %q", cc)
	}

	// Tracking parameters share the entry: the age goes on from the first request.
	status, h3, body3 := get(t, page+"&utm_source=newsletter&fbclid=abc")
	if status != 200 || h3.Get("Cache-Status") != "tifa; hit" || h3.Get("ETag") != h2.Get("ETag") || body3 != body1 || age(h3) < age(h2) {
		t.Errorf("with utm_source: %d %v (Age %v before)", status, h3, h2["Age"])
	}

	// Cookies do not reach the page. A visitor with a cart, a list of seen products and a language
	// cookie gets the page everyone gets.
	status, h4, body4 := get(t, page, "Cookie", "store_cart=abc; shop_seen=oak-tray.ceramic-cup; lang=en-US", "Accept-Language", "en-US")
	if status != 200 || h4.Get("Cache-Status") != "tifa; hit" || body4 != body1 || h4.Get("Set-Cookie") != "" {
		t.Errorf("with cookies: %d %v", status, h4)
	}

	// Every storefront page that has a Cache answers the same way.
	for _, path := range []string{"/store/en-US?e2e=cache", "/store/en-US/products?e2e=cache&sort=price-asc", "/store/zh-CN/products/oak-tray?e2e=cache", "/store/en-US/journal/stock-sync-api?e2e=cache"} {
		_, first, _ := get(t, base+path)
		_, second, _ := get(t, base+path)
		if first.Get("Cache-Status") != "tifa; fwd=miss" || second.Get("Cache-Status") != "tifa; hit" || second.Get("Set-Cookie") != "" {
			t.Errorf("%s: %v then %v", path, first, second)
		}
	}

	// A 404 is not kept, and the visitor's own data and the workbench are not pages with a Cache.
	if _, h, _ := get(t, base+"/store/zh-CN/products/nope"); h.Get("Cache-Status") == "tifa; hit" {
		t.Errorf("a 404 came from the cache: %v", h)
	}
	status, h, mine := get(t, base+"/store/zh-CN/mine", "Cookie", "shop_seen=oak-tray")
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" || !strings.Contains(mine, `"slug":"oak-tray"`) {
		t.Errorf("the visitor's own data: %d %v %.200s", status, h, mine)
	}
	status, h, _ = get(t, base+"/projects/studio")
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("the workbench: %d %v", status, h)
	}
}

// checkStorefrontPageCacheSignedIn checks that a signed-in visitor gets the anonymous page in the
// HTML and their own session in the navigation data. cookie is the session cookie.
func checkStorefrontPageCacheSignedIn(t *testing.T, base, buildID, cookie string) {
	t.Helper()
	page := base + "/store/en-US/journal?e2e=signed-in"
	_, _, anonymous := get(t, page)
	status, h, body := get(t, page, "Cookie", cookie)
	if status != 200 || h.Get("Cache-Status") != "tifa; hit" || body != anonymous || strings.Contains(body, `<html data-session="in"`) || !strings.HasPrefix(body, `<!DOCTYPE html><html lang="en-US">`) {
		t.Errorf("storefront page of a signed-in visitor: %d %v, anonymous HTML: %v", status, h, body == anonymous)
	}
	status, h, data := get(t, dataURL(page, buildID, "11"), "Cookie", cookie)
	if status != 200 || h.Get("Cache-Control") != "private, no-cache" || !strings.Contains(data, `"session":{"signedIn":true`) {
		t.Errorf("navigation data of a signed-in visitor: %d %v %.300s", status, h, data)
	}
}
