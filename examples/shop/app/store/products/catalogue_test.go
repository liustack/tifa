package products

import (
	"slices"
	"testing"
)

// The catalogue is 150 products: the 28 the shop was photographed for, then products that share
// their photos, each a product of its own with a slug and a name in each language no other product
// has, and colours of its own: a set of them that the product whose photos it uses does not have.
func TestTheCatalogueIsItsOwnProducts(t *testing.T) {
	if len(catalogue) != 150 {
		t.Errorf("%d products, want 150", len(catalogue))
	}
	slugs, names := map[string]bool{}, [2]map[string]bool{{}, {}}
	byPhoto := map[string]productRecord{}
	for i, p := range catalogue {
		if slugs[p.slug] {
			t.Errorf("two products %s", p.slug)
		}
		slugs[p.slug] = true
		for l := range 2 {
			if names[l][p.name[l]] {
				t.Errorf("two products named %q", p.name[l])
			}
			names[l][p.name[l]] = true
		}
		if i < 28 {
			if p.photo != p.slug {
				t.Errorf("%s, photographed for the shop, shows the photos of %s", p.slug, p.photo)
			}
			byPhoto[p.photo] = p
			continue
		}
		base, ok := byPhoto[p.photo]
		if !ok {
			t.Errorf("%s shows the photos of %q, which none of the first 28 products has", p.slug, p.photo)
			continue
		}
		if slices.Equal(slices.Sorted(slices.Values(p.colors)), slices.Sorted(slices.Values(base.colors))) {
			t.Errorf("%s comes in the colours of %s, %v", p.slug, base.slug, p.colors)
		}
	}
}
