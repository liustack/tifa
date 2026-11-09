package e2e

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
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

// checkStorefrontIsNotShared checks that no storefront page is shared (it has no Cache): no answer
// has an Age or a Cache-Status, each says Cache-Control: private, no-cache, and the
// render memo may still answer the second request. The visitor's own data and the workbench
// answer the same way.
func checkStorefrontIsNotShared(t *testing.T, base string) {
	t.Helper()
	notKept := func(path string, h map[string][]string) {
		t.Helper()
		if len(h["Cache-Status"]) != 0 || len(h["Age"]) != 0 || strings.Join(h["Cache-Control"], "") != "private, no-cache" {
			t.Errorf("%s is shared: %v", path, h)
		}
	}
	for _, path := range storefrontPages {
		// The render memo may answer the second request, and need not: a delivery date changes at
		// midnight of the store, so two requests on both sides of it are two renders.
		status, first, _ := get(t, base+path)
		status2, second, _ := get(t, base+path)
		if status != 200 || status2 != 200 {
			t.Errorf("%s: %d then %d", path, status, status2)
		}
		notKept(path, first)
		notKept(path, second)
		if v := second.Get("X-Tifa-Render"); v != "vm" && v != "memo" {
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

// checkStorefrontSignedIn checks a storefront page for a signed-in visitor. The page is not
// shared, it carries the visitor's session, the navigation data carries it too, and
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

// shopPages are the pages of the shop that checkEveryShopPageHitsTheMemo asks for twice. The
// confirmation page of a placed order needs an order, and is asked for twice in the checkout test.
var shopPages = []string{
	"/store",
	"/zh-CN/store",
	"/store/products?sort=price-asc&color=moss",
	"/zh-CN/store/products",
	"/store/products/oak-tray",
	"/zh-CN/store/products/oak-tray",
	"/store/journal",
	"/zh-CN/store/journal",
	"/store/journal/stock-sync-api",
	"/zh-CN/store/journal/stock-sync-api",
	"/store/cart",
	"/zh-CN/store/cart",
	"/store/checkout",
	"/zh-CN/store/checkout",
	"/projects/studio",
	"/projects/weekend",
	"/projects/studio/orders/SH-1041",
	"/projects/studio/stats",
	"/projects/weekend/stats",
	"/projects/studio/products",
	"/projects/studio/products/oak-tray",
	"/login",
}

// checkEveryShopPageHitsTheMemo asks for each page of the shop twice, with no write in
// between, and requires the second answer to come from the render memo. A page whose data
// changes on every request (a time with its fraction, a random value, a counter) never hits, and
// this is the check that finds it. header is the headers of the visitor, such as the session
// cookie, and a path in skip is left out for that visitor.
func checkEveryShopPageHitsTheMemo(t *testing.T, base string, skip map[string]string, header ...string) {
	t.Helper()
	for _, page := range shopPages {
		if _, ok := skip[page]; ok {
			continue
		}
		path := page
		if strings.Contains(path, "?") {
			path += "&e2e=hit" + strconv.Itoa(len(header))
		} else {
			path += "?e2e=hit" + strconv.Itoa(len(header))
		}
		secondFromMemo(t, page, func() (int, http.Header) {
			status, h, _ := get(t, base+path, header...)
			return status, h
		})
	}
}

// secondFromMemo asks for a page twice with no write in between and requires the second
// answer to be a 200 from the render memo. A day of the store changes at midnight in either of its
// time zones, and a delivery date is a day of the store, so when that happened between the two
// requests the second render was right: the pair is asked again, once.
func secondFromMemo(t *testing.T, label string, request func() (int, http.Header)) {
	t.Helper()
	started := time.Now()
	status1, _ := request()
	status2, second := request()
	if second.Get("X-Tifa-Render") != "memo" && storeDayChanged(started, time.Now()) {
		status1, _ = request()
		status2, second = request()
	}
	if status1 != 200 || status2 != 200 {
		t.Errorf("%s: %d then %d", label, status1, status2)
		return
	}
	if v := second.Get("X-Tifa-Render"); v != "memo" {
		t.Errorf("%s: the second answer has X-Tifa-Render %q, it must be \"memo\"", label, v)
	}
}

// storeDayChanged says whether the day changed between two times in the time zone of either
// language of the store (Shanghai and Los Angeles).
func storeDayChanged(a, b time.Time) bool {
	for _, name := range []string{"Asia/Shanghai", "America/Los_Angeles"} {
		zone, err := time.LoadLocation(name)
		if err != nil {
			panic(err)
		}
		ay, am, ad := a.In(zone).Date()
		by, bm, bd := b.In(zone).Date()
		if ay != by || am != bm || ad != bd {
			return true
		}
	}
	return false
}
