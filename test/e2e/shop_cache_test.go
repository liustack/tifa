package e2e

import (
	"strings"
	"testing"
)

// storefrontPages are the storefront pages that used to carry a Cache. The query string e2e
// makes the URLs new to this run, since the checks before it opened the others.
var storefrontPages = []string{
	"/store?e2e=cache",
	"/store/products?e2e=cache&sort=price-asc",
	"/zh-CN/store/products/oak-tray?e2e=cache",
	"/zh-CN/store/journal?e2e=cache",
	"/store/journal/stock-sync-api?e2e=cache",
}

// checkStorefrontNoPageCache checks that no storefront page is in the whole-page cache: no answer
// says it came from or went to that cache, each says Cache-Control: private, no-cache, and the
// render cache may still answer the second request. The visitor's own data and the workbench
// answer the same way.
func checkStorefrontNoPageCache(t *testing.T, base string) {
	t.Helper()
	notKept := func(path string, h map[string][]string) {
		t.Helper()
		if len(h["Cache-Status"]) != 0 || len(h["Age"]) != 0 || strings.Join(h["Cache-Control"], "") != "private, no-cache" {
			t.Errorf("%s is in the whole-page cache: %v", path, h)
		}
	}
	for _, path := range storefrontPages {
		// The answers need not be equal: a product page carries the time of the request in its
		// delivery dates, so its data is new each time and the render cache cannot reuse it.
		status, first, _ := get(t, base+path)
		status2, second, _ := get(t, base+path)
		if status != 200 || status2 != 200 {
			t.Errorf("%s: %d then %d", path, status, status2)
		}
		notKept(path, first)
		notKept(path, second)
		if v := second.Get("X-Tifa-Render"); v != "vm" && v != "cache" {
			t.Errorf("%s: X-Tifa-Render %q", path, v)
		}
	}

	// Cookies do not make a page of their own.
	page := base + "/zh-CN/store/journal?e2e=cache"
	_, _, plain := get(t, page)
	_, h, withCookies := get(t, page, "Cookie", "store_cart=abc; shop_seen=oak-tray.ceramic-cup; lang=en-US", "Accept-Language", "en-US")
	notKept(page, h)
	if withCookies != plain {
		t.Errorf("a visitor with cookies got another page")
	}

	// A 404 is not kept, and the visitor's own data and the workbench are plain pages.
	if _, h, _ := get(t, base+"/zh-CN/store/products/nope"); h.Get("Cache-Status") != "" {
		t.Errorf("a 404 has a Cache-Status: %v", h)
	}
	status, h, mine := get(t, base+"/zh-CN/store/mine", "Cookie", "shop_seen=oak-tray")
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" || !strings.Contains(mine, `"slug":"oak-tray"`) {
		t.Errorf("the visitor's own data: %d %v %.200s", status, h, mine)
	}
	status, h, _ = get(t, base+"/projects/studio")
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("the workbench: %d %v", status, h)
	}
}

// checkStorefrontSignedIn checks a storefront page for a signed-in visitor. The page is not in
// the whole-page cache, it carries the visitor's session, the navigation data carries it too, and
// an anonymous visitor who asks afterwards gets the anonymous page, with nothing left over from
// the signed-in one. cookie is the session cookie.
func checkStorefrontSignedIn(t *testing.T, base, buildID, cookie string) {
	t.Helper()
	page := base + "/store/journal?e2e=signed-in"
	_, _, anonymous := get(t, page)
	status, h, body := get(t, page, "Cookie", cookie)
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" || !strings.Contains(body, `<html data-session="in"`) {
		t.Errorf("storefront page of a signed-in visitor: %d %v, %.120s", status, h, body)
	}
	if !strings.Contains(anonymous, `<html data-session="out"`) {
		t.Errorf("the anonymous page does not say that no one is signed in: %.120s", anonymous)
	}
	status, h, again := get(t, page)
	if status != 200 || h.Get("Cache-Status") != "" || h.Get("Cache-Control") != "private, no-cache" || again != anonymous || !strings.Contains(again, `<html data-session="out"`) {
		t.Errorf("an anonymous visitor after a signed-in one: %d %v, same page as before: %v", status, h, again == anonymous)
	}
	status, h, data := get(t, dataURL(page, buildID, "11"), "Cookie", cookie)
	if status != 200 || h.Get("Cache-Control") != "private, no-cache" || !strings.Contains(data, `"session":{"signedIn":true`) {
		t.Errorf("navigation data of a signed-in visitor: %d %v %.300s", status, h, data)
	}
}
