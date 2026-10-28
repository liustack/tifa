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

func TestSearchFiltersByMaterialRatingAndBadge(t *testing.T) {
	// Every material family counts the products that Search finds when it alone is chosen, and a
	// product made of two families is counted in both.
	all := Search(Filter{}, zh)
	if len(all.Materials) < 6 {
		t.Fatalf("%d material facets", len(all.Materials))
	}
	for _, m := range all.Materials {
		r := Search(Filter{Materials: []string{m.ID}}, zh)
		if m.Count == 0 || r.Total != m.Count {
			t.Errorf("material %s: facet %d, search %d", m.ID, m.Count, r.Total)
		}
	}
	ceramic := Search(Filter{Materials: []string{"ceramic"}}, en)
	if !slices.ContainsFunc(ceramic.Products, func(c Card) bool { return c.Slug == "ceramic-cup" }) ||
		slices.ContainsFunc(ceramic.Products, func(c Card) bool { return c.Slug == "pour-over-kettle" }) {
		t.Errorf("ceramic: %+v", ceramic.Products)
	}
	// Rating is a floor in tenths of a star, badges are any of the chosen.
	good := Search(Filter{Rating: 48, Sort: "rating"}, zh)
	if good.Total == 0 || good.Total >= all.Total {
		t.Fatalf("rating 4.8 leaves %d of %d", good.Total, all.Total)
	}
	for _, p := range good.Products {
		if p.Rating < 4.8 {
			t.Errorf("%s has %.1f", p.Slug, p.Rating)
		}
	}
	if r := Search(Filter{Badges: []string{"sale"}}, zh); r.Total != 4 {
		t.Errorf("sale leaves %d", r.Total)
	}
	if r := Search(Filter{Badges: []string{"sale", "new"}}, zh); r.Total != 6 {
		t.Errorf("sale or new leaves %d", r.Total)
	}
	// A facet ignores its own filter and keeps the others.
	both := Search(Filter{Badges: []string{"sale"}, Rating: 48}, zh)
	for _, b := range both.Badges {
		if b.ID == "new" && b.Count != Search(Filter{Badges: []string{"new"}, Rating: 48}, zh).Total {
			t.Errorf("new facet %d under sale and 4.8", b.Count)
		}
	}
}

func TestSearchCountsPriceBands(t *testing.T) {
	for _, l := range []store.Locale{zh, en} {
		r := Search(Filter{InStock: true}, l)
		if len(r.Bands) != 4 {
			t.Fatalf("%s: %d bands", l.Code, len(r.Bands))
		}
		sum := 0
		for _, b := range r.Bands {
			sum += b.Count
			if got := Search(Filter{InStock: true, Min: b.Min, Max: b.Max}, l).Total; got != b.Count {
				t.Errorf("%s band %d to %d: facet %d, search %d", l.Code, b.Min, b.Max, b.Count, got)
			}
		}
		if sum != r.Total {
			t.Errorf("%s: bands add up to %d of %d", l.Code, sum, r.Total)
		}
	}
}

func TestTopRatedStartsWithTheBestAndTheCheaperOfEquals(t *testing.T) {
	top := TopRated(8, zh)
	if len(top) != 8 {
		t.Fatalf("%d products", len(top))
	}
	for i, p := range top[1:] {
		prev := top[i]
		if p.Rating > prev.Rating || (p.Rating == prev.Rating && p.PriceCents < prev.PriceCents) {
			t.Errorf("%s (%.1f, %d) after %s (%.1f, %d)", p.Slug, p.Rating, p.PriceCents, prev.Slug, prev.Rating, prev.PriceCents)
		}
	}
}
