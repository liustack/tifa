package compare

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// mixNames are the names declared in mix.go that are not numbers of the mix: names of the site, of
// the roles and of the kinds of condition of a filter, and a sum of two numbers that are.
var mixNames = []string{"SiteShop", "RoleShopper", "RoleStaff", "SiteBlockPlans", "FilterPrice", "FilterCategory", "FilterRating", "FilterStock", "FilterColor", "FilterMaterial", "FilterBadge"}

// Every number of the mix is declared in mix.go, and the report lists each with its value, read from
// the constant the plans use: a constant or a variable of mix.go that MixFacts does not name fails,
// and so does a fact that names no constant of mix.go.
func TestTheReportListsEveryNumberOfTheMix(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "mix.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST && g.Tok != token.VAR {
			continue
		}
		for _, s := range g.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				declared = append(declared, n.Name)
			}
		}
	}
	var named []string
	for _, fact := range MixFacts() {
		if fact.Name == "" || fact.Value == "" || fact.Source == "" {
			t.Errorf("the fact %+v lacks a name, a value or a source", fact)
		}
		named = append(named, fact.Name)
		if !slices.Contains(declared, fact.Name) {
			t.Errorf("the fact %q names %s, which mix.go does not declare", fact.What, fact.Name)
		}
	}
	for _, n := range declared {
		if !slices.Contains(named, n) && !slices.Contains(mixNames, n) {
			t.Errorf("mix.go declares %s, which the report does not list", n)
		}
	}
	for _, n := range []string{"StoreClicks", "StaffClicks", "StaffLandingShares", "LanguageShares", "ReturningCartLines", "AddPieces", "EditPriceShare", "EditPriceFen", "EditDelivery", "ShopperViewsMore", "StaffViewsMore"} {
		if !slices.Contains(declared, n) {
			t.Errorf("mix.go does not declare %s", n)
		}
	}
}

// The click tables are relative weights, made shares only among the kinds of link a page has (a
// product page has six that add up to 1.1), so the report writes them as weights, not as percents.
func TestTheReportWritesTheClicksAsRelativeWeights(t *testing.T) {
	facts := map[string]MixFact{}
	for _, f := range MixFacts() {
		facts[f.Name] = f
	}
	for _, name := range []string{"StoreClicks", "StaffClicks"} {
		f := facts[name]
		if strings.Contains(f.Value, "%") || !strings.Contains(f.What, "relative weights") || !strings.Contains(f.What, "only among the links the page has") {
			t.Errorf("%s is listed as %q: %q, want relative weights made shares among the links a page has", name, f.What, f.Value)
		}
	}
	if want := "from product: product 30 : cart 25 : list 25 : checkout 10 : home 10 : journal 10"; !strings.Contains(facts["StoreClicks"].Value, want) {
		t.Errorf("StoreClicks is %q, want it to hold %q", facts["StoreClicks"].Value, want)
	}
}

// The shares of the searches, the filters, the sorts and the pages of a list are the approved ones,
// and the report gives each with where it comes from: a published figure with its link, or the word
// assumption. Paging is measured on search results, and the report says so.
func TestTheSearchFilterSortAndPageSharesAreTheApprovedOnes(t *testing.T) {
	if SearchShare != 0.20 || ListFilterShare != 0.25 || ListSortShare != 0.10 {
		t.Errorf("searches %v, filters %v, sorts %v, want 0.20, 0.25 and 0.10", SearchShare, ListFilterShare, ListSortShare)
	}
	if !slices.Equal(FilterConditionShares, []float64{0.60, 0.30, 0.10}) || !slices.Equal(ListPageShares, []float64{0.90, 0.09, 0.01}) {
		t.Errorf("conditions %v, pages %v, want 60:30:10 and 90:9:1", FilterConditionShares, ListPageShares)
	}
	want := map[string]float64{FilterPrice: 25, FilterCategory: 25, FilterRating: 12.5, FilterStock: 12.5, FilterColor: 8.3, FilterMaterial: 8.3, FilterBadge: 8.3}
	if len(FilterKinds) != len(want) {
		t.Errorf("%d kinds of condition, want %d", len(FilterKinds), len(want))
	}
	for _, k := range FilterKinds {
		if want[k.Kind] != k.Weight {
			t.Errorf("the weight of a condition on the %s is %v, want %v", k.Kind, k.Weight, want[k.Kind])
		}
	}
	// The paging figure is of search results and is used on the lists with no search term, because no
	// search word finds more products than a page holds.
	most := 0
	for _, l := range StoreLangs {
		terms := StoreSearchTerms()[l]
		for _, w := range terms.Found {
			most = max(most, termFinds(w))
		}
	}
	if most == 0 || most > PageSize {
		t.Errorf("a search word finds %d products, and a page holds %d: a search can have a second page", most, PageSize)
	}
	facts := map[string]MixFact{}
	for _, f := range MixFacts() {
		facts[f.Name] = f
	}
	for name, in := range map[string][]string{
		"SearchShare":           {"20%", "Constructor", "24%", "https://itbrief.co.uk/story/online-shopper-search-drives-44-of-ecommerce-revenue", "assumption"},
		"ListFilterShare":       {"25%", "assumption", "see a product list"},
		"ListSortShare":         {"10%", "assumption", "see a product list"},
		"FilterConditionShares": {"60%", "30%", "10%", "assumption"},
		"FilterKinds":           {"price 25", "category 25", "rating 12.5", "stock 12.5", "colour 8.3", "material 8.3", "badge 8.3", "assumption", "four price bands"},
		"ListPageShares":        {"90%", "9%", "1%", "Luigi's Box", "https://www.luigisbox.com/blog/product-ranking-data/", "88.7%", "search results", "no search"},
	} {
		f, ok := facts[name]
		if !ok {
			t.Errorf("the report does not list %s", name)
			continue
		}
		for _, want := range in {
			if !strings.Contains(f.What+" "+f.Value+" "+f.Source, want) {
				t.Errorf("%s lacks %q: %+v", name, want, f)
			}
		}
	}
}
