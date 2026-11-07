package products

import (
	"testing"
)

func TestMenuHasEveryCategoryWithItsKindsAndBestProducts(t *testing.T) {
	menu := Menu(zh)
	if len(menu) != len(categories) {
		t.Fatalf("%d groups, want %d", len(menu), len(categories))
	}
	total := 0
	for _, g := range menu {
		total += g.Count
		if len(g.Kinds) == 0 || len(g.Featured) != 2 {
			t.Errorf("%s: %d kinds, %d featured", g.ID, len(g.Kinds), len(g.Featured))
		}
		if g.Featured[0].Rating < g.Featured[1].Rating {
			t.Errorf("%s: featured not best rated first", g.ID)
		}
	}
	if total != len(catalogue) {
		t.Errorf("the groups count %d products, the catalogue has %d", total, len(catalogue))
	}
	if Menu(en)[0].Kinds[0] != "Pour-over" {
		t.Errorf("English kinds: %v", Menu(en)[0].Kinds)
	}
}

func TestEveryProductHasQuestionsInBothLanguages(t *testing.T) {
	for _, p := range catalogue {
		qz, qe := Questions(p.slug, zh), Questions(p.slug, en)
		if len(qz) == 0 || len(qz) != len(qe) {
			t.Fatalf("%s: %d and %d questions", p.slug, len(qz), len(qe))
		}
		if qz[0].Question == qe[0].Question || qz[0].Answer == "" {
			t.Errorf("%s: %+v", p.slug, qz[0])
		}
	}
}

func TestCardsAreNumberedInCatalogueOrder(t *testing.T) {
	for i, p := range catalogue {
		if n := p.card(zh).Number; n != i+1 {
			t.Errorf("%s: number %d, want %d", p.slug, n, i+1)
		}
	}
}

func TestLatestReviewsHavePicturesOfNewProducts(t *testing.T) {
	list := LatestReviews(6, zh)
	newest := Featured(6, zh)
	if len(list) != 6 {
		t.Fatalf("%d reviews", len(list))
	}
	for i, r := range list {
		if r.Product.Slug != newest[i].Slug || len(r.Photos) == 0 || r.Author == "" {
			t.Errorf("review %d: %+v", i, r)
		}
	}
}

func TestBundleHasOtherProductsOfOneSizeInStock(t *testing.T) {
	for _, p := range catalogue {
		for _, b := range Bundle(p.slug, 2, zh) {
			stock, ok := HasVariant(b.Slug, b.Colors[0].ID, "")
			if b.Slug == p.slug || !ok || stock == 0 {
				t.Errorf("%s: bundle has %s (stock %d, one size %v)", p.slug, b.Slug, stock, ok)
			}
		}
	}
	if n := len(Bundle("pour-over-kettle", 2, zh)); n != 2 {
		t.Errorf("the kettle has %d products to go with it", n)
	}
}
