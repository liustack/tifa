package compare

import (
	"math"
	"strings"
	"testing"
)

// Every block of the plans has exactly ClickIDShare of its shopper landings with a click id and
// EmailParamShare with an email parameter, a visit with at most one, the names by their shares, on
// the home, a list and a product alike, and no staff visit has any.
func TestEveryBlockCarriesTheClickIdsAndEmailParamsByQuota(t *testing.T) {
	set := GenerateSitePlanSet(5, 3)
	want := func(share float64) int { return int(math.Round(share * ShopperBlockVisits)) }
	wantNames := map[string]int{}
	for _, c := range ClickIDNames {
		wantNames[c.Name] = want(ClickIDShare * c.Share)
	}
	for _, e := range EmailParamNames {
		wantNames[e.Name] = want(EmailParamShare * e.Share)
	}
	if wantNames["gclid"] != 90 || wantNames["fbclid"] != 45 || wantNames["msclkid"] != 15 || wantNames["_kx"] != 25 || wantNames["mc_eid"] != 25 {
		t.Fatalf("the quotas of a block are %v", wantNames)
	}
	for blk := range 3 {
		names := map[string]int{}
		kinds := map[string]int{}
		for _, p := range set.Plans[blk*SiteBlockPlans : (blk+1)*SiteBlockPlans] {
			for i, s := range p.Steps {
				n := strings.Count(s.Page, "gclid=") + strings.Count(s.Page, "fbclid=") + strings.Count(s.Page, "msclkid=") + strings.Count(s.Page, "_kx=") + strings.Count(s.Page, "mc_eid=")
				switch {
				case n > 1:
					t.Fatalf("plan %d carries more than one parameter: %s", p.ID, s.Page)
				case n == 1 && (i > 0 || p.Role != RoleShopper):
					t.Fatalf("plan %d: %s carries a parameter and is not the landing of a shopper", p.ID, s.Page)
				}
			}
			name := landingParam(p.Steps[0].Page)
			if name == "" {
				continue
			}
			v, _ := landingValueOf(name)
			if !strings.HasSuffix(p.Steps[0].Page, name+"="+v.Placeholder) || len(v.Placeholder) != LandingValueLengths[name] {
				t.Fatalf("plan %d: %s lacks the placeholder of %s, of %d characters", p.ID, p.Steps[0].Page, name, LandingValueLengths[name])
			}
			names[name]++
			_, k := StorePageKind(p.Steps[0].Page)
			kinds[k]++
			if strings.Contains(stripLanding(p.Steps[0].Page), name) {
				t.Fatalf("stripLanding keeps %s in %s", name, p.Steps[0].Page)
			}
		}
		for name, n := range wantNames {
			if names[name] != n {
				t.Errorf("block %d: %d landings carry %s, want %d", blk, names[name], name, n)
			}
		}
		for _, k := range []string{StoreKindHome, StoreKindList, StoreKindProduct} {
			if kinds[k] == 0 {
				t.Errorf("block %d: no %s landing carries a parameter", blk, k)
			}
		}
	}
}

// The pages of the plans do not depend on the parameters, a list that has a query gets the parameter
// after it, and the clicks of a visit follow the links of the page without the parameter.
func TestALandingParameterLeavesThePageAlone(t *testing.T) {
	if got := withLanding("/store/products?sort=newest", "gclid"); got != "/store/products?sort=newest&gclid="+placeholderOf("tifa-gclid-", 95) {
		t.Errorf("a list with a query: %s", got)
	}
	if got := withLanding("/store", "mc_eid"); got != "/store?mc_eid=tifa0mceid" {
		t.Errorf("the home: %s", got)
	}
	for _, url := range []string{"/store/products?sort=newest&fbclid=" + placeholderOf("tifa-fbclid-", 61), "/zh-CN/store?_kx=" + placeholderOf("tifa-kx-", 40)} {
		base := stripLanding(url)
		if strings.Contains(base, "clid") || strings.Contains(base, "_kx") {
			t.Errorf("stripLanding(%s) = %s", url, base)
		}
		if len(StoreLinks(url)) == 0 || len(StoreLinks(url)) != len(StoreLinks(base)) {
			t.Errorf("%s has %d links, %s has %d", url, len(StoreLinks(url)), base, len(StoreLinks(base)))
		}
	}
	if stripLanding("/store/products?q=cup&gclid=x&sort=newest") != "/store/products?q=cup&sort=newest" {
		t.Error("stripLanding drops the other parameters of the list")
	}
}

// The numbers of the landing parameters are in the mix table with a source that says what is assumed.
func TestTheMixTableListsTheLandingParameters(t *testing.T) {
	got := map[string]MixFact{}
	for _, f := range MixFacts() {
		got[f.Name] = f
	}
	for name, value := range map[string]string{"ClickIDShare": "15%", "EmailParamShare": "5%", "ClickIDNames": "gclid 60%, fbclid 30%, msclkid 10%", "EmailParamNames": "_kx 50%, mc_eid 50%"} {
		f, ok := got[name]
		if !ok || !strings.Contains(f.Value, value) {
			t.Errorf("%s: %+v, want a value with %q", name, f, value)
		}
	}
	if !strings.Contains(got["ClickIDNames"].Source, "assumption") || !strings.Contains(got["EmailParamNames"].Source, "assumption") || !strings.Contains(got["LandingValueLengths"].Source, "assumption") {
		t.Error("the shares of the names and the lengths of the values are assumptions and the table says so")
	}
}

// A round that plays the plans checks the share of landings with a click id and with an email
// parameter, as it checks the other shares of the mix.
func TestTheMixOfTheSiteIsCheckedForTheLandingParameters(t *testing.T) {
	w := siteWorkload(t, 2, 1, "shop")
	var whole Round
	w.plan(&whole, 0, 2*w.blockVisits())
	if m := whole.Mix; m.ClickIDs != 2*want150() || m.EmailParams != 2*50 {
		t.Fatalf("two blocks plan %d click ids and %d email parameters", m.ClickIDs, m.EmailParams)
	}
	for name, mod := range map[string]func(*PlannedMix){
		"click ids":    func(m *PlannedMix) { m.ClickIDs += m.Shoppers / 100 },
		"email params": func(m *PlannedMix) { m.EmailParams = 0 },
	} {
		m := *whole.Mix
		mod(&m)
		if m.why(w.Scenario) == "" {
			t.Errorf("a plan off in its %s has the mix", name)
		}
	}
}

func want150() int { return int(math.Round(ClickIDShare * ShopperBlockVisits)) }
