package compare

import (
	"math"
	"slices"
	"strings"
	"testing"
)

// The plans of the one site: in every block, 1000 shopper visits with the mix of mix.go exactly
// (pages a visit, landing pages, languages, adds to the cart, checkouts, returning visitors) and the
// staff visits of the back office, each page view a click on a link of the page before.
func TestSitePlanSetHasTheMixInEveryBlock(t *testing.T) {
	a, b, c := GenerateSitePlanSet(7, 2), GenerateSitePlanSet(7, 2), GenerateSitePlanSet(8, 2)
	same := func(x, y Plan) bool {
		return slices.Equal(x.Steps, y.Steps) && x.Role == y.Role && x.Returning == y.Returning && slices.Equal(x.Writes, y.Writes)
	}
	if a.Site != SiteShop || a.Blocks != 2 || !slices.EqualFunc(a.Plans, b.Plans, same) {
		t.Fatal("the same seed gave other plans")
	}
	if slices.EqualFunc(a.Plans, c.Plans, same) {
		t.Fatal("another seed gave the same plans")
	}
	workbench := map[string]bool{}
	for _, p := range AllPages() {
		workbench[p.URL] = true
	}
	want := func(share float64) int { return int(math.Round(share * ShopperBlockVisits)) }
	for blk := range 2 {
		plans := a.Plans[blk*SiteBlockPlans : (blk+1)*SiteBlockPlans]
		var shoppers, staff, shopperViews, staffViews, adds, checkouts, returning int
		landings, langs := map[string]int{}, map[string]int{}
		for i, p := range plans {
			if p.ID != blk*SiteBlockPlans+i || p.Block != blk {
				t.Fatalf("plan %d of block %d has id %d and block %d", i, blk, p.ID, p.Block)
			}
			if p.Steps[0].Kind != StepLoad {
				t.Fatalf("plan %d starts with a %s", p.ID, p.Steps[0].Kind)
			}
			for j, s := range p.Steps[1:] {
				links := StoreLinks
				if p.Role == RoleStaff {
					links = Links
				}
				if s.Kind != StepClick || !slices.ContainsFunc(links(p.Steps[j].Page), func(l Link) bool { return l.To == s.Page }) {
					t.Fatalf("plan %d: %s -> %s is no click on a link", p.ID, p.Steps[j].Page, s.Page)
				}
			}
			switch p.Role {
			case RoleStaff:
				staff++
				staffViews += len(p.Steps)
				if len(p.Steps) > StaffViewsMax || p.Returning || len(p.Writes) > 0 {
					t.Errorf("staff plan %d: %d page views, returning %v, writes %v", p.ID, len(p.Steps), p.Returning, p.Writes)
				}
				for _, s := range p.Steps {
					if !workbench[s.Page] {
						t.Fatalf("staff plan %d shows %s, not a page of the back office", p.ID, s.Page)
					}
				}
			case RoleShopper:
				shoppers++
				shopperViews += len(p.Steps)
				if len(p.Steps) > ShopperViewsMax {
					t.Errorf("shopper plan %d has %d page views", p.ID, len(p.Steps))
				}
				lang, kind := StorePageKind(p.Steps[0].Page)
				landings[kind]++
				langs[lang]++
				for _, s := range p.Steps {
					if l, k := StorePageKind(s.Page); k == "" || l != lang {
						t.Fatalf("shopper plan %d shows %s in a visit in %s", p.ID, s.Page, lang)
					}
				}
				if p.Returning {
					returning++
				}
				// An add follows a product page view, a checkout follows the checkout page after the add.
				for k, w := range p.Writes {
					_, on := StorePageKind(p.Steps[w.After].Page)
					switch {
					case w.Kind == WriteAdd && k == 0 && on == StoreKindProduct:
						adds++
					case w.Kind == WriteCheckout && k == 1 && on == StoreKindCheckout && w.After > p.Writes[0].After:
						checkouts++
					default:
						t.Fatalf("shopper plan %d: write %d is a %s after a %s page view: %v", p.ID, k, w.Kind, on, p.Writes)
					}
				}
			default:
				t.Fatalf("plan %d has the role %q", p.ID, p.Role)
			}
		}
		if shoppers != ShopperBlockVisits || staff != StaffBlockPlans {
			t.Errorf("block %d: %d shopper and %d staff plans", blk, shoppers, staff)
		}
		if shopperViews != int(math.Round(ShopperViewsMean*ShopperBlockVisits)) || staffViews != StaffViewsMean*StaffBlockPlans {
			t.Errorf("block %d: %d shopper and %d staff page views", blk, shopperViews, staffViews)
		}
		if adds != want(AddToCartShare) || checkouts != want(CheckoutShare) || returning != want(ReturningShare) {
			t.Errorf("block %d: %d visits add to the cart (want %d), %d check out (want %d), %d return (want %d)",
				blk, adds, want(AddToCartShare), checkouts, want(CheckoutShare), returning, want(ReturningShare))
		}
		for _, l := range LandingShares {
			if landings[l.Kind] != want(l.Share) {
				t.Errorf("block %d: %d visits land on a %s, want %d", blk, landings[l.Kind], l.Kind, want(l.Share))
			}
		}
		if langs[StoreLangs[0]] != ShopperBlockVisits/2 || langs[StoreLangs[1]] != ShopperBlockVisits/2 {
			t.Errorf("block %d: languages %v", blk, langs)
		}
	}
}

// The products a visit lands on follow a Zipf law over the catalogue: the product of rank k is seen
// about 1/k^s as often as the first.
func TestLandingProductsFollowTheZipfLaw(t *testing.T) {
	set := GenerateSitePlanSet(3, 4)
	count := map[string]int{}
	n := 0
	for _, p := range set.Plans {
		if _, kind := StorePageKind(p.Steps[0].Page); p.Role == RoleShopper && kind == StoreKindProduct {
			slug := p.Steps[0].Page[strings.LastIndex(p.Steps[0].Page, "/")+1:]
			count[slug]++
			n++
		}
	}
	if n < 1000 {
		t.Fatalf("%d visits land on a product", n)
	}
	var h float64
	for k := range StoreProducts {
		h += math.Pow(float64(k+1), -ProductZipf)
	}
	for k, slug := range StoreProducts[:5] {
		want := math.Pow(float64(k+1), -ProductZipf) / h
		if got := float64(count[slug]) / float64(n); math.Abs(got-want) > 0.25*want {
			t.Errorf("%s, rank %d, lands %.3f of the product visits, want %.3f", slug, k+1, got, want)
		}
	}
}

// A click to a product picks it by its popularity: among the product links of a page, the weights
// are as the Zipf weights of the products.
func TestStoreLinksPickProductsByTheirPopularity(t *testing.T) {
	checked := 0
	for _, url := range StoreVisitPages() {
		var products []Link
		for _, l := range StoreLinks(url) {
			if _, k := StorePageKind(l.To); k == StoreKindProduct {
				products = append(products, l)
			}
		}
		for i := 1; i < len(products); i++ {
			a, b := products[0], products[i]
			want := productWeight(productOf(a.To)) / productWeight(productOf(b.To))
			if got := a.Weight / b.Weight; math.Abs(got-want) > 1e-9*want {
				t.Fatalf("%s: %s against %s weighs %v, want %v", url, a.To, b.To, got, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no page has two product links")
	}
}

// A shopper visit is 4 page views on average, one full load and three clicks, exactly in every block,
// so that the full loads are a quarter of the page views. Its length is geometric, from 1 to 12, and
// the mix table of the report names the sources of the 4.
func TestAShopperVisitIsFourPageViews(t *testing.T) {
	if ShopperViewsMean != 4 || ShopperViewsMax != 12 {
		t.Fatalf("a shopper visit is %v page views on average and %d at most", ShopperViewsMean, ShopperViewsMax)
	}
	set := GenerateSitePlanSet(1, 3)
	for blk := range 3 {
		lengths := map[int]int{}
		visits, views := 0, 0
		for _, p := range set.Plans[blk*SiteBlockPlans : (blk+1)*SiteBlockPlans] {
			if p.Role == RoleShopper {
				visits++
				views += len(p.Steps)
				lengths[len(p.Steps)]++
			}
		}
		if views != 4*visits {
			t.Errorf("block %d: %d page views in %d shopper visits, not 4 each", blk, views, visits)
		}
		// A geometric length of mean 4 ends after the first page a quarter of the time and reaches 9
		// pages or more in about one visit in ten.
		long := 0
		for n := 9; n <= ShopperViewsMax; n++ {
			long += lengths[n]
		}
		if one := float64(lengths[1]) / float64(visits); one < 0.18 || one > 0.32 || long < visits/20 {
			t.Errorf("block %d: lengths %v are not geometric of mean 4", blk, lengths)
		}
	}
	var facts []string
	for _, f := range MixFacts() {
		if strings.HasPrefix(f.Name, "ShopperViews") {
			facts = append(facts, f.Value+" "+f.Source)
		}
	}
	all := strings.Join(facts, "\n")
	for _, want := range []string{"1 full load and 3 clicks", "https://web.dev/blog/crux-experimental-soft-navs", "Contentsquare", "4.3"} {
		if !strings.Contains(all, want) {
			t.Errorf("the page views of a shopper visit in the mix table lack %q:\n%s", want, all)
		}
	}
	for _, gone := range []string{"2.6", "top 10%", "66.5%"} {
		if strings.Contains(all, gone) {
			t.Errorf("the page views of a shopper visit in the mix table still say %q:\n%s", gone, all)
		}
	}
}
