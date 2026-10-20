package products

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

var (
	zh, _ = store.FindLocale("zh-CN")
	en, _ = store.FindLocale("en-US")
)

func TestSearchFiltersSortsAndPages(t *testing.T) {
	all := Search(Filter{}, zh)
	if all.Total != len(catalogue) || len(all.Products) != PageSize || all.Pages != 2 || all.Filter.Sort != "featured" || all.Filter.Page != 1 {
		t.Fatalf("no filter: %d of %d on %d pages, sort %q", len(all.Products), all.Total, all.Pages, all.Filter.Sort)
	}
	if second := Search(Filter{Page: 2}, zh); len(second.Products) != len(catalogue)-PageSize {
		t.Errorf("second page holds %d", len(second.Products))
	}
	moss := Search(Filter{Colors: []string{"moss"}, Sort: "price-asc"}, en)
	if moss.Total != 11 || moss.Products[0].Slug != "dot-notebook" {
		t.Fatalf("moss by price: %d, first %s", moss.Total, moss.Products[0].Slug)
	}
	for i, p := range moss.Products[1:] {
		if p.PriceCents < moss.Products[i].PriceCents || !slices.ContainsFunc(p.Colors, func(c Color) bool { return c.ID == "moss" }) {
			t.Errorf("%s at %d is out of order or not moss", p.Slug, i+1)
		}
	}
	// A facet counts what the other filters leave: the colour facet ignores the colour filter.
	for _, f := range moss.Colors {
		if f.ID == "moss" && f.Count != 11 {
			t.Errorf("moss facet %d", f.Count)
		}
	}
	sum := 0
	for _, f := range moss.Categories {
		sum += f.Count
	}
	if sum != moss.Total {
		t.Errorf("category facets add up to %d, want %d", sum, moss.Total)
	}
	// Price bounds are whole units of the locale's currency, and the query matches both languages.
	if r := Search(Filter{Min: 50}, en); slices.ContainsFunc(r.Products, func(c Card) bool { return c.PriceCents < 5000 }) {
		t.Error("min 50 dollars lets in cheaper products")
	}
	if Search(Filter{Query: "kettle"}, zh).Total == 0 || Search(Filter{Query: "手冲"}, en).Total == 0 {
		t.Error("the query does not match a name in the other language")
	}
	if r := Search(Filter{Sort: "dearest"}, zh); r.Filter.Sort != "featured" {
		t.Errorf("unknown sort kept as %q", r.Filter.Sort)
	}
}

func TestProductsAndVariants(t *testing.T) {
	p, err := GetProduct("pour-over-kettle", en)
	if err != nil || p.Name != "Morning Pour-Over Kettle" || p.PriceCents != 4620 || len(p.Care) == 0 || len(p.Specs) == 0 {
		t.Fatalf("kettle %+v %v", p, err)
	}
	if _, err := GetProduct("nope", en); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("unknown product: %v", err)
	}
	if _, ok := HasVariant("pour-over-kettle", "charcoal", ""); !ok {
		t.Error("the kettle comes in charcoal")
	}
	if _, ok := HasVariant("pour-over-kettle", "pink", ""); ok {
		t.Error("the kettle does not come in pink")
	}
	if _, ok := HasVariant("linen-apron", "oat", ""); ok {
		t.Error("the apron needs a size")
	}
	related := Related("pour-over-kettle", 4, zh)
	if len(related) != 4 || slices.ContainsFunc(related, func(c Card) bool { return c.Slug == "pour-over-kettle" }) {
		t.Errorf("related %+v", related)
	}
}

func TestSearchKeepsNumbersWithinRange(t *testing.T) {
	// A page past the last shows the last, also one so large that its offset would overflow.
	for _, page := range []int{3, math.MaxInt} {
		r := Search(Filter{Page: page}, zh)
		if r.Filter.Page != r.Pages || len(r.Products) != len(catalogue)-PageSize {
			t.Errorf("page %d: shows page %d of %d with %d products", page, r.Filter.Page, r.Pages, len(r.Products))
		}
	}
	if r := Search(Filter{Min: math.MaxInt}, en); r.Total != 0 || r.Filter.Page != 1 || r.Pages != 1 {
		t.Errorf("min above every price: %d products, page %d of %d", r.Total, r.Filter.Page, r.Pages)
	}
	if r := Search(Filter{Max: math.MaxInt}, en); r.Total != len(catalogue) {
		t.Errorf("max above every price: %d products", r.Total)
	}
}
