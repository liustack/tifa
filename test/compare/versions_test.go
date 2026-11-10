package compare

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// visitOf is a visit that adds walnut-pen to the cart after its first page view and checks out after its
// third.
func visitOf() VisitWrites {
	add := storeAdd(rand.New(rand.NewPCG(1, 2)), "/store", pageProduct("/store/products/walnut-pen"))
	checkout := StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/store/checkout", Body: []byte(`{}`), Status: 303}
	return VisitWrites{Writes: []PlannedWrite{
		{After: 0, Seq: 5, Kind: WriteAdd, Store: &add},
		{After: 2, Seq: 5, Kind: WriteCheckout, Store: &checkout},
	}}
}

func TestACheckoutChangesTheStockOfTheProductsInItsCart(t *testing.T) {
	eff := versionEffects(1, visitOf())
	if len(eff) != 1 || eff[0].After != 2 {
		t.Fatalf("effects %+v, want the one of the checkout after view 2 (an add changes no public page)", eff)
	}
	// The demonstration cart and the pen the visit added, which keep their price.
	want := []string{"admin:studio", "ceramic-cup:s", "linen-apron:s", "pour-over-kettle:s", "reading-candle:s", "walnut-pen:s"}
	if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
		t.Errorf("a checkout changes %v, want %v", got, want)
	}
}

func TestStaffWritesChangeWhatTheyEdit(t *testing.T) {
	seed := uint64(7)
	byPrice, byStock := "", ""
	for seq := range 200 {
		w := PlannedWrite{After: 1, Seq: seq, Kind: WriteEdit, Project: storefrontShop}
		slug, change := productChange(seed, w, func(string) int { return 0 })
		eff := versionEffects(seed, VisitWrites{Writes: []PlannedWrite{w}})
		if _, price := change["priceCents"]; price {
			want := []string{"admin:studio", slug + ":p"}
			slices.Sort(want)
			if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
				t.Fatalf("an edit of the price of %s changes %v, want %v", slug, got, want)
			}
			byPrice = slug
		} else {
			want := []string{"admin:studio", slug + ":s"}
			slices.Sort(want)
			if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
				t.Fatalf("an edit of the stock of %s changes %v, want %v", slug, got, want)
			}
			byStock = slug
		}
	}
	if byPrice == "" || byStock == "" {
		t.Fatalf("200 edits held no price (%q) or no stock (%q)", byPrice, byStock)
	}
	// A ship changes the back office of its shop and nothing the storefront shows: the order was
	// placed before the clock.
	ship := PlannedWrite{After: 0, Seq: 3, Kind: WriteShip, Project: "studio"}
	eff := versionEffects(seed, VisitWrites{Writes: []PlannedWrite{ship}})
	if len(eff) != 1 || !slices.Equal(eff[0].Keys, []string{"admin:studio"}) {
		t.Errorf("a ship changes %+v, want the back office of its shop alone", eff)
	}
}

func TestAReturningVisitsCartIsInItsCheckout(t *testing.T) {
	v := visitOf()
	v.Setup = returningCart(1, 5, Plan{Steps: []PlanStep{{StepLoad, "/store"}}})
	eff := versionEffects(1, v)
	got := map[string]bool{}
	for _, k := range eff[0].Keys {
		got[k] = true
	}
	// The cart of a returning visit holds what the setup added besides the demonstration cart.
	for _, r := range v.Setup {
		slug := r.Path[len("/store/products/"):]
		if !got[slug+":s"] {
			t.Errorf("the checkout of a returning visit does not change the stock of %s, which the setup put in its cart: %v", slug, eff[0].Keys)
		}
	}
}
