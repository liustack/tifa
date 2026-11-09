package compare

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// mixNames are the names declared in mix.go that are not numbers of the mix: names of the site and
// of the roles, and a sum of two numbers that are.
var mixNames = []string{"SiteShop", "RoleShopper", "RoleStaff", "SiteBlockPlans"}

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
