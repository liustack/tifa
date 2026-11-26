package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// checkRoutersAgree sends URLs that spell the item page /shops/north/items/apple in odd ways, or
// something close to it, and checks that the router of the program (Go) and the router that renders
// the page (TanStack Router in the VM) agree on each. A page that Go routes is rendered by the VM
// for the same URL, so when the two differ the answer is a 200 with the wrong page, a 5xx, or a
// 404 for a URL that has an answer. The answer for a URL is the item page, the 404 page, or the
// item page after a redirect to the canonical spelling, and nothing else.
func checkRoutersAgree(t *testing.T, base string) {
	t.Helper()
	// The shop in the path is a free parameter, so North and nor/th name shops too: what is told is
	// the page. The Go mux cleans a path with an encoded .. and redirects, which the client follows
	// to the list of the items of the shop.
	isItem := func(body string) bool { return strings.Contains(body, "<article><h2>Apple") }
	isList := func(body string) bool { return strings.Contains(body, `<a href="/shops/north/items/apple">apple</a>`) }
	for _, target := range []string{
		"/shops/north/items/apple",
		"/Shops/north/items/apple",
		"/shops/North/items/apple",
		"/shops/north/items/APPLE",
		"/shops/north/items/%61pple",
		"/shops/north/items/%41pple",
		"/%73hops/north/items/apple",
		"/shops/north/items/apple/",
		"/shops/north/items/apple//",
		"/shops//north/items/apple",
		"//shops/north/items/apple",
		"/shops/north/./items/apple",
		"/shops/north/items/./apple",
		"/shops/north/items/%2e",
		"/shops/north/items/%2E%2E",
		"/shops/north/items/%2e%2e/items/apple",
		"/shops/north/%2E%2E/north/items/apple",
		"/shops/north/items/apple%00",
		"/shops/north/items/%00apple",
		"/shops/north/items/a%2Fb",
		"/shops/nor%2Fth/items/apple",
		"/shops/north/items/apple%2F",
		"/shops/north/items/apple%5C",
		"/shops/north/items/app%C3%A9",
		"/shops/north/items/apple?x=1",
		"/shops/north/items/apple#frag",
	} {
		status, _, body := get(t, base+target)
		switch {
		case status == http.StatusNotFound:
			// Both routers say there is no such page.
		case status == http.StatusOK && (isItem(body) || isList(body)):
			// Both say it is a page of the shop.
		default:
			_, shown, _ := strings.Cut(body, "<body")
			t.Errorf("GET %s: %d, which is no item page and no 404:\n<body%.600s", target, status, shown)
		}
	}
}
