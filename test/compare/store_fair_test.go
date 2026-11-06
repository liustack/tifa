package compare_test

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The fairness checks of the storefront. They run next to the ones of the workbench, in
// TestEveryRequestComputesAndRenders and TestNoCachingBetweenRequests.

// storeFairPages are storefront pages that stand for the rest: the home, a filtered list, two
// products, the cart, the checkout, the journal and a post, in both languages.
var storeFairPages = []string{
	zh,
	en + "/products?q=cup&sort=price-asc",
	zh + "/products/pour-over-kettle",
	en + "/products/oak-tray",
	zh + "/cart",
	en + "/checkout",
	zh + "/journal",
	en + "/journal/theme-css",
}

// readCounts reads the counters of a server, from one address or from several (Tifa's two
// counters are on two addresses).
func readAllCounts(t *testing.T, bases []string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, base := range bases {
		for k, n := range readCounts(t, base) {
			out[k] = n
		}
	}
	return out
}

// storeComputes asks one server for the pages of storeFairPages and for a write followed by the
// cart page, and checks that every request did its work again. It returns how often FindLocale
// ran for each, which the caller compares between the servers: when it is the same, the Go code
// and the port look up the language the same number of times.
//
// For a page of a Node app one request runs the Layout of the language once, the Load of the page
// once and renders once, and the Root Layout and the project Layout of the workbench not at all.
// For Tifa the page is rendered in a VM (X-Tifa-Render). Then five more requests do five times as
// much.
func storeComputes(t *testing.T, name, base string, countBases []string, node bool, write func(string) string) map[string]int64 {
	t.Helper()
	perCase := map[string]int64{}
	v := newVisitor(t, base, write)
	check := func(label string, run func() reply) {
		t.Helper()
		before := readAllCounts(t, countBases)
		r := run()
		one := diffCounts(before, readAllCounts(t, countBases))
		if r.status >= 400 {
			t.Errorf("%s, %s: answered %d: %s", name, label, r.status, r.body)
		}
		isPage := strings.HasPrefix(label, "GET ")
		// A page looks the language up in its Layout and in its Load. A write does not, it changes the cart,
		// which the page read after it shows.
		if isPage && one["findLocale"] <= 0 {
			t.Errorf("%s, %s: one request never looked up the language: %v", name, label, one)
		}
		perCase[label+" (findLocale)"] = one["findLocale"]
		perCase[label+" (products built)"] = one["products"]
		if node && isPage {
			for key, want := range map[string]int64{"shell": 1, "project": 0, "page": 1, "renders": 1} {
				if one[key] != want {
					t.Errorf("%s, %s: one request ran %s %d times, want %d: %v", name, label, key, one[key], want, one)
				}
			}
		}
		if !node && isPage {
			if render := r.header.Get("X-Tifa-Render"); render != "vm" {
				t.Errorf("%s, %s: X-Tifa-Render is %q, want vm", name, label, render)
			}
		}
		before = readAllCounts(t, countBases)
		for range requests {
			again := run()
			if !node && isPage && again.header.Get("X-Tifa-Render") != "vm" {
				t.Errorf("%s, %s: X-Tifa-Render is %q, want vm", name, label, again.header.Get("X-Tifa-Render"))
			}
		}
		for key, n := range diffCounts(before, readAllCounts(t, countBases)) {
			if n != requests*one[key] {
				t.Errorf("%s, %s: %d requests made %d of %s, want %d", name, label, requests, n, key, requests*one[key])
			}
		}
	}
	for _, path := range storeFairPages {
		check("GET "+path, func() reply { return v.get(path) })
	}

	// A write, then the cart: the write computes (it looks the language up and changes the cart),
	// and the page after it is made again from the cart, not kept from before.
	cart := v.get(zh + "/cart")
	check("POST add to cart", func() reply {
		return v.do(request{method: "POST", path: zh + "/products/pour-over-kettle", body: `{"color":"mist","size":"","quantity":1}`, call: true})
	})
	after := v.get(zh + "/cart")
	if d := compare.Diff(maskView(compare.ParseView(cart.body)), maskView(compare.ParseView(after.body))); d == "" {
		t.Errorf("%s: the cart page is the same after an add to the cart", name)
	}
	check("PATCH quantity", func() reply {
		return v.do(request{method: "PATCH", path: zh + "/cart", body: `{"line":"pour-over-kettle.mist","quantity":3}`, call: true})
	})
	check("GET "+zh+"/cart after a write", func() reply { return v.get(zh + "/cart") })
	return perCase
}

// storeNoCache checks, for one server, that a storefront page and the answer to a write carry no
// header that lets a cache keep them, and that a page read after a write is not the page read
// before it.
func storeNoCache(t *testing.T, name, base string, write func(string) string) {
	t.Helper()
	v := newVisitor(t, base, write)
	noStore := name == "nextjs"
	headers := func(label string, r reply) {
		t.Helper()
		if r.status >= 400 {
			t.Errorf("%s, %s: answered %d", name, label, r.status)
		}
		for _, h := range []string{"ETag", "Last-Modified", "Expires", "Age"} {
			if val := r.header.Get(h); val != "" {
				t.Errorf("%s answers %s with %s: %s", name, label, h, val)
			}
		}
		cache := r.header.Get("Cache-Control")
		switch {
		case strings.HasPrefix(label, "GET") && noStore && !strings.Contains(cache, "no-store"):
			t.Errorf("%s answers %s with Cache-Control %q, want no-store", name, label, cache)
		case strings.HasPrefix(label, "GET") && !noStore && cache != "":
			t.Errorf("%s answers %s with Cache-Control %q", name, label, cache)
		case !strings.HasPrefix(label, "GET") && cache != "" && !strings.Contains(cache, "no-store"):
			t.Errorf("%s answers %s with Cache-Control %q, which lets a cache keep it", name, label, cache)
		}
	}
	for _, path := range storeFairPages {
		headers("GET "+path, v.get(path))
	}
	// Two reads of the cart around a write: what the page shows follows the cart.
	read := func() compare.View {
		r := v.get(zh + "/cart")
		headers("GET "+zh+"/cart", r)
		return maskView(compare.ParseView(r.body))
	}
	first, second := read(), read()
	if d := compare.Diff(first, second); d != "" {
		t.Errorf("%s: the cart page changed with no write:\n%s", name, d)
	}
	headers("POST add", v.do(request{method: "POST", path: zh + "/products/linen-apron", body: `{"color":"oat","size":"M","quantity":1}`, call: true}))
	third := read()
	if compare.Diff(second, third) == "" {
		t.Errorf("%s: the cart page is the same after an add to the cart, a cache served it", name)
	}
	headers("PATCH quantity", v.do(request{method: "PATCH", path: zh + "/cart", body: `{"line":"linen-apron.oat.M","quantity":4}`, call: true}))
	fourth := read()
	if compare.Diff(third, fourth) == "" {
		t.Errorf("%s: the cart page is the same after a change of quantity, a cache served it", name)
	}
	// Another visitor does not see the cart of this one.
	other := newVisitor(t, base, write)
	if d := compare.Diff(maskView(compare.ParseView(other.get(zh+"/cart").body)), first); d != "" {
		t.Errorf("%s: a visitor with no cookies got a cart page that differs from the first one:\n%s", name, d)
	}
}
