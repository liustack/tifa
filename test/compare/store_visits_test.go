package compare_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// TestPlannedStoreWritesAreTakenByEveryApp sends the storefront writes of 150 visit plans (every
// page view followed by a write), each visit with the cookies of its own, to Tifa and to the three
// apps, at the path each takes them on. Every write must answer with the status the plan expects
// (200 for an add and a quantity change, 204 for a coupon, 303 to the page of the order for a
// checkout, which is not followed) and the cart page after the last write of a visit must be the
// one the plan leaves: empty after a checkout, else holding the lines of the plan.
func TestPlannedStoreWritesAreTakenByEveryApp(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	plans := compare.GenerateStorePlans(1, 150, 4)
	located := regexp.MustCompile(`^(?:/zh-CN)?/store/checkout\?placed=SS-[A-Z0-9]{10}$`)
	for _, name := range []string{"tifa-nocache", "nextjs", "start", "reactrouter"} {
		var spec compare.Spec
		for _, s := range env.Specs() {
			if s.Name == name {
				spec = s
			}
		}
		server, err := spec.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		sent := map[string]int{}
		for _, p := range plans {
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			for _, w := range compare.PlanStoreWrites(1, p, 1) {
				path := spec.StoreWrite(w.Store.Path)
				req, _ := http.NewRequestWithContext(ctx, w.Store.Method, server.Base+path, strings.NewReader(string(w.Store.Body)))
				req.Header.Set("Content-Type", "application/json")
				res, err := client.Do(req)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != w.Store.Status {
					t.Fatalf("%s, plan %d, write after view %d: %s %s %s answered %d, want %d: %.200s", name, p.ID, w.After, w.Store.Method, path, w.Store.Body, res.StatusCode, w.Store.Status, body)
				}
				if w.Kind == compare.WriteCheckout && !located.MatchString(res.Header.Get("Location")) {
					t.Fatalf("%s: a checkout sent the visitor to %q", name, res.Header.Get("Location"))
				}
				sent[w.Kind]++
			}
		}
		server.Stop()
		for _, kind := range []string{compare.WriteAdd, compare.WriteQuantity, compare.WriteCoupon, compare.WriteCheckout} {
			if sent[kind] == 0 {
				t.Errorf("%s: no %s write was sent", name, kind)
			}
		}
		t.Logf("%s took %v", name, sent)
	}
}

// TestTifaWithItsCachesOffNeverAnswersAStorefrontPageFromOne asks for pages whose route has Cache
// twice. With every cache off, both answers are renders in a VM and
// the Cache-Status is a miss, never a hit. With only the whole-page cache off, no answer comes from it (no Cache-Status)
// but the second render comes from the render cache. The shop as shipped is the control: its second
// answer is a hit of the whole-page cache, so the check can fail.
func TestTifaWithItsCachesOffNeverAnswersAStorefrontPageFromOne(t *testing.T) {
	env := setup(t)
	buildShop(t, env)
	ctx := context.Background()
	pages := []string{"/store", "/zh-CN/store/products", "/store/products/oak-tray", "/store/journal", "/zh-CN/store/journal/theme-css"}
	type answer struct{ status, cache, render string }
	ask := func(base, path string) answer {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return answer{http.StatusText(res.StatusCode), res.Header.Get("Cache-Status"), res.Header.Get("X-Tifa-Render")}
	}
	for _, c := range []struct {
		name string
		// cache is the Cache-Status of every answer, render the X-Tifa-Render values a second answer may
		// have, and rendered whether some page must have its second answer from the render cache.
		firstCache, againCache string
		againRender            []string
		rendered               bool
	}{
		{"tifa", "tifa; fwd=miss", "tifa; hit", []string{"cache"}, true},
		{"tifa-nopage", "", "", []string{"cache", "vm"}, true},
		{"tifa-nocache", "tifa; fwd=miss", "tifa; fwd=miss", []string{"vm"}, false},
	} {
		server, err := mustTarget(t, env, c.name).Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		fromRenderCache := false
		for _, page := range pages {
			first, again := ask(server.Base, page), ask(server.Base, page)
			if first.cache != c.firstCache || first.render != "vm" {
				t.Errorf("%s: first request of %s answered %+v, want Cache-Status %q and a render in a VM", c.name, page, first, c.firstCache)
			}
			if again.cache != c.againCache || !slices.Contains(c.againRender, again.render) {
				t.Errorf("%s: second request of %s answered %+v, want Cache-Status %q and a render of %v", c.name, page, again, c.againCache, c.againRender)
			}
			fromRenderCache = fromRenderCache || again.render == "cache"
		}
		if fromRenderCache != c.rendered {
			t.Errorf("%s: a second answer came from the render cache: %v, want %v", c.name, fromRenderCache, c.rendered)
		}
		server.Stop()
	}
}

// findProduct finds the product with a slug in the data of a page: the object that has its slug, its
// stock and its sizes.
func findProduct(v any, slug string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["slug"] == slug && x["stock"] != nil {
			if _, ok := x["sizes"]; ok {
				return x
			}
		}
		for _, e := range x {
			if p := findProduct(e, slug); p != nil {
				return p
			}
		}
	case []any:
		for _, e := range x {
			if p := findProduct(e, slug); p != nil {
				return p
			}
		}
	}
	return nil
}

// TestStoreVariantsAreTheShops reads the data of every product page from the shop and checks that
// StoreVariants holds its stock, its colours and its sizes, and that it lists every product.
func TestStoreVariantsAreTheShops(t *testing.T) {
	env := setup(t)
	buildShop(t, env)
	ctx := context.Background()
	server, err := mustTarget(t, env, "tifa-nocache").Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	id, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	if len(compare.StoreVariants) != len(compare.StoreProducts) {
		t.Errorf("%d variants for %d products", len(compare.StoreVariants), len(compare.StoreProducts))
	}
	for _, slug := range compare.StoreProducts {
		if !slices.ContainsFunc(compare.StoreVariants, func(v compare.StoreProduct) bool { return v.Slug == slug }) {
			t.Errorf("StoreVariants lacks %s", slug)
		}
	}
	for _, v := range compare.StoreVariants {
		res, err := http.Get(server.Base + "/store/products/" + v.Slug + "?_tifa=" + id + ".11")
		if err != nil {
			t.Fatal(err)
		}
		var page any
		err = json.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("%s: %d %v", v.Slug, res.StatusCode, err)
		}
		p := findProduct(page, v.Slug)
		if p == nil {
			t.Fatalf("%s: no product in the data of its page", v.Slug)
		}
		var colors, sizes []string
		for _, c := range p["colors"].([]any) {
			colors = append(colors, c.(map[string]any)["id"].(string))
		}
		if s, ok := p["sizes"].([]any); ok {
			for _, e := range s {
				sizes = append(sizes, e.(string))
			}
		}
		if int(p["stock"].(float64)) != v.Stock || !slices.Equal(colors, v.Colors) || !slices.Equal(sizes, v.Sizes) {
			t.Errorf("%s: the shop has stock %v, colours %v, sizes %v. StoreVariants has %d, %v, %v", v.Slug, p["stock"], colors, sizes, v.Stock, v.Colors, v.Sizes)
		}
	}
}
