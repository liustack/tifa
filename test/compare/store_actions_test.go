package compare

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"math"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// blockActions counts, by hand and from the plans alone, what the shopper plans of a block do with
// the product list.
type blockActions struct {
	searches, found, none, unique int
	lists                         int // visits that see a list
	filters                       int
	conditions                    [4]int // filters by the number of conditions they set
	sorts                         int
	paged                         int            // visits that see a list of more than one page with no search term
	second, third                 int            // of them, those whose deepest page is the second, and the third or deeper
	landedSecond, landedThird     int            // of those, the visits that got there with no page action: they landed there
	document, router              map[string]int // the actions by act: document loads and client navigations
}

func countBlock(t *testing.T, plans []Plan) blockActions {
	t.Helper()
	c := blockActions{document: map[string]int{}, router: map[string]int{}}
	for _, p := range plans {
		if p.Role != RoleShopper {
			continue
		}
		var searches, filters, sorts, pages int
		list, deepest := false, 0
		for i, s := range p.Steps {
			if _, kind := StorePageKind(s.Page); kind == StoreKindList {
				list = true
				// The page of the list, read from its URL, and whether the list has several pages, which only
				// the shop's rules tell: the model of the list, with the data the shop starts with.
				_, query, _ := strings.Cut(stripLanding(s.Page), "?")
				v, err := url.ParseQuery(query)
				if err != nil {
					t.Fatalf("plan %d: %s: %v", p.ID, s.Page, err)
				}
				page := 1
				if v.Has("page") {
					if page, err = strconv.Atoi(v.Get("page")); err != nil || page < 2 {
						t.Fatalf("plan %d: %s has the page %q", p.ID, s.Page, v.Get("page"))
					}
				}
				price, inStock := shopPrices()
				m := ModelList(stripLanding(s.Page), price, inStock)
				if m.Filter.Page != page {
					t.Fatalf("plan %d: %s shows page %d of %d", p.ID, s.Page, m.Filter.Page, m.Pages)
				}
				if strings.TrimSpace(v.Get("q")) == "" && m.Pages > 1 {
					deepest = max(deepest, page)
				}
			}
			if i == 0 {
				continue
			}
			switch s.Act {
			case ActSearch:
				searches++
				c.document[s.Act]++
				switch {
				case s.Term == SearchPlaceholder:
					c.unique++
				case termFinds(s.Term) == 0:
					c.none++
				default:
					c.found++
				}
			case ActFilter:
				filters++
				c.document[s.Act]++
				c.conditions[len(conditionsOf(s.Set))]++
			case ActForm:
				filters++
				c.router[s.Act]++
				c.conditions[len(conditionsOf(s.Set))]++
			case ActSort:
				sorts++
				c.router[s.Act]++
			case ActPage:
				pages++
				c.document[s.Act]++
			case ActLink:
				c.router[s.Act]++
			default:
				t.Fatalf("plan %d: page view %d has the act %q", p.ID, i, s.Act)
			}
		}
		if searches > 1 || filters > 1 || sorts > 1 || pages > 2 || (filters > 0 && sorts > 0) || (pages > 0 && filters+sorts > 0) {
			t.Fatalf("plan %d: %d searches, %d filters, %d sorts, %d pages", p.ID, searches, filters, sorts, pages)
		}
		c.searches += searches
		c.filters += filters
		c.sorts += sorts
		if list {
			c.lists++
		}
		// The depth of a visit is the deepest page it sees of a list of several pages with no search word,
		// however it got there: by a page action or by landing on that page.
		switch {
		case deepest == 0:
		case deepest == 1:
			c.paged++
		case deepest == 2:
			c.paged++
			c.second++
			if pages == 0 {
				c.landedSecond++
			}
		default:
			c.paged++
			c.third++
			if pages == 0 {
				c.landedThird++
			}
		}
	}
	return c
}

// Every block of the plans has the searches, the filters, the sorts and the pages of the mix by quota,
// exactly: 200 shopper visits search once, 180 for a word that finds a product, 10 for a word of the
// list that finds nothing and 10 for a word of their own. Of the visits that see a list a quarter
// filter it, with one, two and three conditions by 60 : 30 : 10, and a tenth change the sort. Of the
// visits that see a list of several pages with no search term, 9% go to the second page and 1% on to
// the third. What the mix had before is as it was: 4 page views a visit, the landings, the adds and
// the checkouts, the returning visitors, the lengths of the visits.
func TestEveryBlockPlansTheSearchesFiltersSortsAndPagesByQuota(t *testing.T) {
	round := func(x float64) int { return int(math.Round(x)) }
	for _, seed := range []uint64{1, 2, 3} {
		set := GenerateSitePlanSet(seed, 2)
		if err := checkActionQuotas(&set); err != nil {
			t.Errorf("seed %d: %v", seed, err)
		}
		for b := range set.Blocks {
			plans := set.Plans[b*SiteBlockPlans : b*SiteBlockPlans+ShopperBlockVisits]
			c := countBlock(t, plans)
			if c.searches != 200 || c.found != 180 || c.none != 10 || c.unique != 10 {
				t.Errorf("seed %d block %d: %d searches, %d for a word that finds a product, %d for one that finds nothing, %d for a word of their own, want 200, 180, 10 and 10", seed, b, c.searches, c.found, c.none, c.unique)
			}
			if c.filters != round(ListFilterShare*float64(c.lists)) || c.sorts != round(ListSortShare*float64(c.lists)) {
				t.Errorf("seed %d block %d: %d visits see a list, %d filter it and %d change the sort, want %d and %d", seed, b, c.lists, c.filters, c.sorts, round(ListFilterShare*float64(c.lists)), round(ListSortShare*float64(c.lists)))
			}
			want := quotaCounts(c.filters, FilterConditionShares)
			if c.conditions[1] != want[0] || c.conditions[2] != want[1] || c.conditions[3] != want[2] {
				t.Errorf("seed %d block %d: the filters set %v conditions, want %v", seed, b, c.conditions[1:], want)
			}
			// The depth is by the deepest page a visit sees. The visits that land on a second page count, and
			// the page actions fill what is missing to the quota. Were more to land there than the quota,
			// none would be taken away (the landings are a quota of their own) and no page action added.
			wantSecond, wantThird := max(round(ListPageShares[1]*float64(c.paged)), c.landedSecond), max(round(ListPageShares[2]*float64(c.paged)), c.landedThird)
			if c.second != wantSecond || c.third != wantThird {
				t.Errorf("seed %d block %d: of %d visits that see a list of several pages, %d see the second page and no deeper (%d land there) and %d the third or deeper (%d land there), want %d and %d", seed, b, c.paged, c.second, c.landedSecond, c.third, c.landedThird, wantSecond, wantThird)
			}
			if c.lists < 500 || c.paged < 300 || c.third == 0 || c.landedSecond == 0 || c.landedSecond >= c.second {
				t.Errorf("seed %d block %d: %d visits see a list, %d one of several pages, %d see a third page, %d of %d that stop at the second land there: too few to show the quotas", seed, b, c.lists, c.paged, c.third, c.landedSecond, c.second)
			}
			if seed == 1 && b == 0 {
				t.Logf("a block: %d visits see a list, %d filter (%v conditions), %d sort, %d see a list of several pages, of which %d see the second page and no deeper (%d land there) and %d the third or deeper (%d land there). Document loads %v, client navigations %v", c.lists, c.filters, c.conditions[1:], c.sorts, c.paged, c.second, c.landedSecond, c.third, c.landedThird, c.document, c.router)
			}
		}
	}
}

// The lengths of the visits and where they land are drawn before anything a visit does with the list,
// so they are what they were before the visits searched: the same lengths, landings of the same kind
// in the same language, visit by visit, as the plans of the same seed with no search and no filter.
func TestTheSearchesAndFiltersLeaveTheLengthsAndTheLandingsAsTheyWere(t *testing.T) {
	// The lengths of block 0 of seed 1 as they were before the searches, and a hash of the length, the
	// language and the kind of landing of every shopper visit of two blocks of three seeds, both taken
	// from the plans the generator made before it dealt searches.
	before := map[int]int{1: 231, 2: 195, 3: 138, 4: 110, 5: 85, 6: 62, 7: 36, 8: 29, 9: 23, 10: 18, 11: 19, 12: 54}
	for seed, want := range map[uint64]string{1: "6829035fe266", 2: "a7e9c6406ebf", 7: "5ee7e82801c7"} {
		set := GenerateSitePlanSet(seed, 2)
		h := sha256.New()
		lens := map[int]int{}
		for _, p := range set.Plans {
			if p.Role != RoleShopper {
				continue
			}
			lang, kind := StorePageKind(p.Steps[0].Page)
			fmt.Fprintf(h, "%d %d %s %s\n", p.ID, len(p.Steps), lang, kind)
			if p.Block == 0 {
				lens[len(p.Steps)]++
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)[:6]); got != want {
			t.Errorf("seed %d: the lengths and the landings of the visits hash to %s, and were %s", seed, got, want)
		}
		if seed == 1 && fmt.Sprint(lens) != fmt.Sprint(before) {
			t.Errorf("the lengths of the visits are %v, and were %v", lens, before)
		}
	}
}

// A search is typed into the search box of the header of the page on screen, which every page has: the
// visit has two page views or more, searches once, at any page view after its landing, and the page it
// gets is the list of the word, written as a browser writes a form. The two lists a visit can land on
// that have a word in their URL are landings and no searches.
func TestASearchIsOneActionOfAVisitOfTwoPageViewsOrMore(t *testing.T) {
	set := GenerateSitePlanSet(4, 2)
	at := map[bool]int{}
	landedOnASearch := 0
	for _, p := range set.Plans {
		if p.Role != RoleShopper {
			continue
		}
		if listFilterOf(p.Steps[0].Page).Query != "" {
			landedOnASearch++
			if p.Steps[0].Act != "" {
				t.Errorf("plan %d lands on %s with the act %q", p.ID, p.Steps[0].Page, p.Steps[0].Act)
			}
		}
		for i, s := range p.Steps {
			if s.Act != ActSearch {
				continue
			}
			lang, _ := StorePageKind(p.Steps[0].Page)
			if i == 0 || len(p.Steps) < 2 || s.Kind != StepClick || s.Page != StoreBase(lang)+"/products?q="+formEncode(s.Term) || s.Term == "" {
				t.Errorf("plan %d of %d page views searches for %q at page view %d and gets %s", p.ID, len(p.Steps), s.Term, i, s.Page)
			}
			if s.Term != SearchPlaceholder {
				terms := StoreSearchTerms()[lang]
				if !slices.Contains(terms.Found, s.Term) && !slices.Contains(terms.None, s.Term) {
					t.Errorf("plan %d, in %s, searches for %q, which is no word of its language", p.ID, lang, s.Term)
				}
			}
			at[i == len(p.Steps)-1]++
		}
	}
	if landedOnASearch == 0 || at[true] == 0 || at[false] == 0 {
		t.Errorf("%d visits land on a list with a word, %d searches are the last page view of their visit and %d are not", landedOnASearch, at[true], at[false])
	}
	if got := formEncode("tea cup & 杯*-._~"); got != "tea+cup+%26+%E6%9D%AF*-._%7E" {
		t.Errorf("a form writes the word as %q", got)
	}
}

// Every page view after the landing says what the visitor does on the page on screen, and the page
// has it: a link (any page), the search box (any page), and on a product list a plain link that adds
// one condition, the form of the sidebar that sets one to three, the sort and the link to the next
// page. The model of the list gives the links of a list and the page each action leads to. A filter
// leads to a list that shows a product. No link leads from a list to another list: only a filter, a
// sort, a page or a search does.
func TestEveryActionOfAPlanIsOneThePageHas(t *testing.T) {
	price, inStock := shopPrices()
	set := GenerateSitePlanSet(5, 2)
	acts := map[string]int{}
	check := func(p Plan, steps []PlanStep) {
		for j := 1; j < len(steps); j++ {
			prev, s := stripLanding(steps[j-1].Page), steps[j]
			_, prevKind := StorePageKind(prev)
			_, kind := StorePageKind(s.Page)
			acts[s.Act]++
			fail := func(why string) {
				t.Errorf("plan %d: page view %d, %s from %s to %s: %s", p.ID, j, s.Act, prev, s.Page, why)
			}
			if s.Kind != StepClick {
				fail("it is no click")
			}
			if s.Act == ActLink {
				if !slices.ContainsFunc(StoreLinks(prev), func(l Link) bool { return l.To == s.Page }) {
					fail("the page has no such link")
				}
				if prevKind == StoreKindList && kind == StoreKindList {
					fail("a link from a list to a list")
				}
				continue
			}
			if s.Act == ActSearch {
				continue
			}
			if prevKind != StoreKindList || kind != StoreKindList {
				fail("it is not from a list to a list")
				continue
			}
			from, to := ModelList(prev, price, inStock), ModelList(s.Page, price, inStock)
			plain := func(part string) bool {
				return slices.ContainsFunc(from.Links(), func(l ListLink) bool { return l.To == s.Page && l.Part == part && l.Plain })
			}
			switch s.Act {
			case ActFilter:
				if n := len(conditionsOf(s.Set)); n != 1 || !plain(s.Part) || !slices.Contains([]string{ListPartCategory, ListPartQuick, ListPartBand}, s.Part) {
					fail(fmt.Sprintf("%d conditions, and the part %q has no such plain link", n, s.Part))
				}
			case ActForm:
				if n := len(conditionsOf(s.Set)); n < 1 || n > 3 || s.Part != "" {
					fail(fmt.Sprintf("the form sets %d conditions", n))
				}
				if n := len(conditionsOf(s.Set)); n == 1 && slices.ContainsFunc(from.Links(), func(l ListLink) bool { return l.To == s.Page && l.Plain }) {
					fail("one condition that a plain link of the page sets goes through the form")
				}
			case ActSort:
				if to.Filter.Sort != s.Sort || s.Sort == "featured" || from.Filter.Sort == s.Sort || !slices.Contains(listSorts, s.Sort) {
					fail("the sort is " + to.Filter.Sort)
				}
			case ActPage:
				if !plain(ListPartPage) || to.Filter.Page != from.Filter.Page+1 || strings.TrimSpace(from.Filter.Query) != "" {
					fail(fmt.Sprintf("page %d of %d after page %d", to.Filter.Page, to.Pages, from.Filter.Page))
				}
			}
			if s.Act == ActFilter || s.Act == ActForm {
				// The list after it has every condition the action sets besides the ones it had, and shows a product.
				if to.Total == 0 || to.Total > from.Total || s.Page != applyConditions(from.Filter, conditionsOf(s.Set)) || s.Page == prev {
					fail(fmt.Sprintf("%q leads to %s with %d of %d products", s.Set, applyConditions(from.Filter, conditionsOf(s.Set)), to.Total, from.Total))
				}
			}
		}
	}
	for _, p := range set.Plans {
		if p.Role != RoleShopper {
			continue
		}
		check(p, p.Steps)
		for _, v := range p.Variants {
			check(p, v)
		}
	}
	for _, act := range []string{ActLink, ActSearch, ActFilter, ActForm, ActSort, ActPage} {
		if acts[act] == 0 {
			t.Errorf("no page view of the plans is a %s", act)
		}
	}
}

// A visit that searches for a word of the list is planned PlanVariants times, the visit itself and
// its variants, which differ in the word alone and in the pages after it: as many page views, each of
// the same kind of page and reached by the same act, the same conditions and sort, and the writes
// after the same page views. A visit with a word of its own has none: its word is put in at the
// replay, and finds nothing whatever it is.
func TestAVisitThatSearchesHasVariantsThatDifferInTheWordAlone(t *testing.T) {
	set := GenerateSitePlanSet(6, 2)
	if err := checkVariants(&set); err != nil {
		t.Fatal(err)
	}
	withWrites, otherWord, otherPages := 0, 0, 0
	for _, p := range set.Plans {
		at := slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Act == ActSearch })
		if at < 0 || p.Steps[at].Term == SearchPlaceholder {
			if len(p.Variants) != 0 {
				t.Errorf("plan %d has %d variants and no search for a word of the list", p.ID, len(p.Variants))
			}
			continue
		}
		if len(p.Variants) != PlanVariants-1 {
			t.Fatalf("plan %d has %d variants, want %d besides itself", p.ID, len(p.Variants), PlanVariants-1)
		}
		found := termFinds(p.Steps[at].Term) > 0
		for _, v := range p.Variants {
			if len(v) != len(p.Steps) || !slices.Equal(v[:at], p.Steps[:at]) {
				t.Fatalf("plan %d: a variant has %d page views, or differs before the search", p.ID, len(v))
			}
			if (termFinds(v[at].Term) > 0) != found || v[at].Term == SearchPlaceholder {
				t.Errorf("plan %d: a variant searches for %q, of another class than %q", p.ID, v[at].Term, p.Steps[at].Term)
			}
			if v[at].Term != p.Steps[at].Term {
				otherWord++
			}
			for j := at; j < len(v); j++ {
				a, b := p.Steps[j], v[j]
				_, ka := StorePageKind(a.Page)
				_, kb := StorePageKind(b.Page)
				if a.Kind != b.Kind || a.Act != b.Act || a.Part != b.Part || a.Set != b.Set || a.Sort != b.Sort || ka != kb {
					t.Errorf("plan %d: page view %d is %+v in the visit and %+v in a variant", p.ID, j, a, b)
				}
				if j > at && a.Page != b.Page {
					otherPages++
				}
			}
			// The writes are after the same page views, and those are pages they can follow.
			for _, w := range p.Writes {
				if _, k := StorePageKind(v[w.After].Page); w.Kind == WriteAdd && k != StoreKindProduct || w.Kind == WriteCheckout && k != StoreKindCheckout {
					t.Errorf("plan %d: a variant has a %s after a %s page view", p.ID, w.Kind, k)
				}
			}
		}
		if len(p.Writes) > 0 {
			withWrites++
		}
	}
	if withWrites == 0 || otherWord == 0 || otherPages == 0 {
		t.Errorf("%d visits with variants write, %d variants have another word, %d page views of variants are other pages: the test shows too little", withWrites, otherWord, otherPages)
	}
	// A set of plans whose variant is not the visit with another word is refused where it is loaded.
	broken := func(change func(p *Plan)) error {
		set := GenerateSitePlanSet(6, 1)
		for i := range set.Plans {
			if len(set.Plans[i].Variants) > 0 && len(set.Plans[i].Steps) > 2 {
				change(&set.Plans[i])
				break
			}
		}
		dir := t.TempDir()
		data, _ := json.Marshal(set)
		os.WriteFile(PlansPath(dir), data, 0o644)
		_, err := ReadPlans(dir)
		return err
	}
	if err := broken(func(*Plan) {}); err != nil {
		t.Fatalf("the plans as they are made: %v", err)
	}
	for name, change := range map[string]func(p *Plan){
		"a variant fewer":            func(p *Plan) { p.Variants = p.Variants[1:] },
		"a shorter variant":          func(p *Plan) { p.Variants[0] = p.Variants[0][:len(p.Variants[0])-1] },
		"a variant with another act": func(p *Plan) { v := slices.Clone(p.Variants[0]); v[len(v)-1].Act = ActSort; p.Variants[0] = v },
		"a variant with a word of its own": func(p *Plan) {
			at := slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Act == ActSearch })
			v := slices.Clone(p.Variants[0])
			v[at].Term, v[at].Page = SearchPlaceholder, strings.Replace(v[at].Page, formEncode(v[at].Term), SearchPlaceholder, 1)
			p.Variants[0] = v
		},
	} {
		if err := broken(change); err == nil || !strings.Contains(err.Error(), "variant") {
			t.Errorf("%s: error %v, want one about the variant", name, err)
		}
	}
}

// Plans made before the searches say nothing of what a visit does on a page, and are refused where
// they are loaded, with what to do.
func TestPlansWithoutTheActionsOfTheMixAreRefusedWhenLoaded(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	for i := range set.Plans {
		set.Plans[i].Variants = nil
		for j := range set.Plans[i].Steps {
			s := &set.Plans[i].Steps[j]
			s.Act, s.Part, s.Term, s.Set, s.Sort = "", "", "", "", ""
		}
	}
	dir := t.TempDir()
	data, _ := json.Marshal(set)
	os.WriteFile(PlansPath(dir), data, 0o644)
	if _, err := ReadPlans(dir); err == nil || !strings.Contains(err.Error(), "compare plans") {
		t.Errorf("error %v, want one that says to make the plans again", err)
	}
}

// A visit that checks out sees a product and later the checkout. Its pages are walked again until it
// does, and its search stays where it was: the searches are dealt before, by quota.
func TestAVisitThatChecksOutKeepsItsSearch(t *testing.T) {
	both := 0
	for _, seed := range []uint64{1, 2, 3, 4, 5, 6} {
		set := GenerateSitePlanSet(seed, 2)
		for _, p := range set.Plans {
			if len(p.Writes) == 2 && slices.ContainsFunc(p.Steps, func(s PlanStep) bool { return s.Act == ActSearch }) {
				both++
			}
		}
	}
	if both == 0 {
		t.Error("no visit of 12 blocks both searches and checks out")
	}
}

// The traces are bound to what the plans are made from: the stamp of browse.sh lists every file the
// plans come from, and the rate of Next.js is bound to them too.
func TestTheStampOfTheTracesHasEveryFileThePlansAreMadeFrom(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(env.Root + "/test/compare/browse.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"plan.go", "landing.go", "mix.go", "store.go", "store_plan.go", "store_actions.go", "store_list.go", "store_terms.go", "store_links.json"} {
		if !strings.Contains(string(script), " test/compare/"+f+" ") {
			t.Errorf("the stamp of the traces does not list test/compare/%s", f)
		}
		if !slices.Contains(methodFiles, f) {
			t.Errorf("the rate of Next.js is not bound to %s", f)
		}
	}
}

// How deep a visit pages is the deepest page it sees of a list of several pages with no search word,
// not how many page actions it takes: a visit that lands on a second page has seen the second page,
// and one that goes on from there has seen the third.
func TestTheDepthOfAVisitIsTheDeepestPageItSees(t *testing.T) {
	land := func(page string, more ...PlanStep) Plan {
		return Plan{Role: RoleShopper, Steps: append([]PlanStep{{Kind: StepLoad, Page: page}}, more...)}
	}
	next := func(page string) PlanStep {
		return PlanStep{Kind: StepClick, Page: page, Act: ActPage, Part: ListPartPage}
	}
	for _, tc := range []struct {
		name                                   string
		plan                                   Plan
		paged, second, third, landed2, landed3 int
	}{
		{"the first page of the whole list", land("/store/products"), 1, 0, 0, 0, 0},
		{"a product", land("/store/products/oak-tray"), 0, 0, 0, 0, 0},
		{"a search, which has one page", land("/store/products?q=cup"), 0, 0, 0, 0, 0},
		{"a landing on the second page", land("/store/products?page=2"), 1, 1, 0, 1, 0},
		{"the first page, then the second", land("/store/products", next("/store/products?page=2")), 1, 1, 0, 0, 0},
		{"a landing on the second page, then the third", land("/store/products?page=2", next("/store/products?page=3")), 1, 0, 1, 0, 0},
		{"the first page, the second and the third", land("/zh-CN/store/products", next("/zh-CN/store/products?page=2"), next("/zh-CN/store/products?page=3")), 1, 0, 1, 0, 0},
		{"a landing on the third page", land("/store/products?sort=newest&page=3"), 1, 0, 1, 0, 1},
	} {
		var c ActionCounts
		c.Add(tc.plan)
		if c.PagedVisits != tc.paged || c.SecondPages != tc.second || c.ThirdPages != tc.third || c.SecondPagesLanded != tc.landed2 || c.ThirdPagesLanded != tc.landed3 {
			t.Errorf("%s: %+v, want %d visits that see a list of several pages, %d to the second page (%d landed) and %d to the third (%d landed)", tc.name, c, tc.paged, tc.second, tc.landed2, tc.third, tc.landed3)
		}
	}
}
