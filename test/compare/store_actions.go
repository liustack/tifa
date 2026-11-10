package compare

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// What a shopper visit does with the product list besides clicking links: it searches, filters, sorts
// and pages, by the quotas of the mix (mix.go), exactly in every block. GenerateSitePlanSet deals them
// in this order, each step on the plans the steps before it left:
//
//  1. The lengths of the visits and where they land, as before.
//  2. The searches: SearchShare of the shopper visits, among those of two page views or more, search
//     once, at a page view after the landing drawn alike among them. The class of the word is dealt by
//     quota and the word drawn in the class (store_terms.go). The search box is in the header of every
//     page, so a search can follow any page.
//  3. The other page views: a click on a link of the page before, by StoreClicks. A product list has
//     the links the model of the list gives it (ModelList), a search result as any other, and no link
//     leads from a list to a list: only the actions here do.
//  4. The writes: the visits that check out walk their pages again until they see a product and later
//     the checkout, and keep their search where it is. Then the adds.
//  5. The filters: ListFilterShare of the visits that see a list filter one, with one to three
//     conditions by FilterConditionShares, of kinds by FilterKinds.
//  6. The sorts: ListSortShare of the visits that see a list change the sort of one.
//  7. The pages: of the visits that see a list of more than one page with no search term,
//     ListPageShares go to the second page, and on to the third.
//  8. The variants of the visits that search (PlanVariants).
//
// A filter, a sort or a page is an action on a list the visit is on: it takes the place of the page view
// after that list, and the page views after it are walked again from the page it leads to, the search
// of the visit staying where it is. So that the quotas come out exact, a visit does one of the three at
// most (an assumption: nothing was found on how they go together), and the action comes after the
// page views its writes follow, which stay as they are. Which visits see a list does not change from
// step 5 on, since the list an action is taken on stays in the visit, and neither does which see a
// list of several pages in step 7.

// PlanVariants is in how many forms a visit that searches for a word of the list is planned and
// recorded: the visit and its variants, which have another word each. The words of a language follow a
// Zipf law with a long tail, and a round plays the same plans again and again: with one word a visit,
// the words of a round would be the few of its plans, and a server that keeps pages would have seen
// every one after the warm-up. The replay picks one form for each played visit.
const PlanVariants = 5

// PlanCondition is one condition a filter adds to a product list: its kind (FilterPrice and the others
// of mix.go) and its value. A price is a band of the page, "100-199" with the lower or the upper bound
// left out for none ("-99", "400-"), a category, a colour, a family of materials and a badge are the
// id the shop has, a rating is the floor in tenths of a star ("48") and the stock is "1".
type PlanCondition struct{ Kind, Value string }

// conditionsOf reads the conditions of a step (PlanStep.Set).
func conditionsOf(set string) []PlanCondition {
	var out []PlanCondition
	for part := range strings.SplitSeq(set, ",") {
		if kind, value, ok := strings.Cut(part, ":"); ok {
			out = append(out, PlanCondition{kind, value})
		}
	}
	return out
}

// setOf writes conditions as a step holds them.
func setOf(conds []PlanCondition) string {
	parts := make([]string, len(conds))
	for i, c := range conds {
		parts[i] = c.Kind + ":" + c.Value
	}
	return strings.Join(parts, ",")
}

// formEncode writes a value as a browser writes it into the URL of a form it sends with GET
// (application/x-www-form-urlencoded): letters, digits and * - . _ as they are, a space as +, and every
// other byte escaped.
func formEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("*-._", c) >= 0:
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// searchStep is the page view of a search for term in a language: the list the form of the header
// leads to (the search box of examples/shop/app/store/layout_.tsx, a form that sends q to the product
// list of the language).
func searchStep(lang, term string) PlanStep {
	return PlanStep{Kind: StepClick, Page: StoreBase(lang) + "/products?q=" + formEncode(term), Act: ActSearch, Term: term}
}

// linkStep is the page view of a click on a link to page.
func linkStep(page string) PlanStep { return PlanStep{Kind: StepClick, Page: page, Act: ActLink} }

// planLists keeps the model of each list the plans asked for, by URL.
var planLists sync.Map

// planList is the product list at a URL as the shop shows it with the data it starts with, which is
// what a browser records: the catalogue file, before a restock or any write.
func planList(page string) ListPage {
	page = stripLanding(page)
	if p, ok := planLists.Load(page); ok {
		return p.(ListPage)
	}
	start := shopStart()
	p := modelList(page, start.price, start.inStock, start.picks)
	planLists.Store(page, p)
	return p
}

// shopStart is the data a shop starts with, read once: the prices and the stock of the catalogue file,
// and the best rated products at those prices.
var shopStart = sync.OnceValue(func() (s struct {
	price   map[string]int
	inStock map[string]bool
	picks   []string
}) {
	s.price, s.inStock = shopPrices()
	s.picks = topRatedOf(storeCatalogue().records, s.price, listPicks)
	return s
})

// with is the values of the form with one more condition.
func (v listValues) with(c PlanCondition) listValues {
	switch c.Kind {
	case FilterPrice:
		v.min, v.max, _ = strings.Cut(c.Value, "-")
	case FilterCategory:
		v.category = c.Value
	case FilterRating:
		v.rating = c.Value
	case FilterStock:
		v.inStock = c.Value == "1"
	case FilterColor:
		v.colors = append(slices.Clone(v.colors), c.Value)
	case FilterMaterial:
		v.materials = append(slices.Clone(v.materials), c.Value)
	case FilterBadge:
		v.badges = append(slices.Clone(v.badges), c.Value)
	}
	return v
}

// has says whether the values hold a condition of a kind already.
func (v listValues) has(kind string) bool {
	switch kind {
	case FilterPrice:
		return v.min != "" || v.max != ""
	case FilterCategory:
		return v.category != ""
	case FilterRating:
		return v.rating != ""
	case FilterStock:
		return v.inStock
	case FilterColor:
		return len(v.colors) > 0
	case FilterMaterial:
		return len(v.materials) > 0
	case FilterBadge:
		return len(v.badges) > 0
	}
	return true
}

// applyConditions is the URL of the list f with the conditions added, at its first page: where the
// plain link of a condition leads, and where the form leads when it is applied with them (listHref
// and onSubmit of FilterForm in examples/shop/app/store/products/page.tsx).
func applyConditions(f listFilter, conds []PlanCondition) string {
	v := f.values()
	for _, c := range conds {
		v = v.with(c)
	}
	return v.href(f.Lang, 1)
}

// filterOptions are the conditions of a kind a visitor can add to the list p: none when the list has
// a condition of the kind already (another value would not narrow it), and else the values the page
// offers that leave a product, as the form greys out a value that leaves none. A price is one of the
// bands of the page.
func filterOptions(p ListPage, kind string) []PlanCondition {
	if p.Filter.values().has(kind) {
		return nil
	}
	var all []PlanCondition
	add := func(value string) { all = append(all, PlanCondition{kind, value}) }
	switch kind {
	case FilterPrice:
		for _, b := range p.Bands {
			lo, hi := "", ""
			if b.Min > 0 {
				lo = strconv.Itoa(b.Min)
			}
			if b.Max > 0 {
				hi = strconv.Itoa(b.Max)
			}
			add(lo + "-" + hi)
		}
	case FilterCategory:
		for _, id := range listCategories {
			add(id)
		}
	case FilterRating:
		for _, floor := range listRatingFloors {
			add(strconv.Itoa(floor))
		}
	case FilterStock:
		add("1")
	case FilterColor:
		for _, id := range listColors {
			add(id)
		}
	case FilterMaterial:
		for _, g := range listMaterials {
			add(g.id)
		}
	case FilterBadge:
		for _, id := range listBadges {
			add(id)
		}
	}
	var out []PlanCondition
	for _, c := range all {
		if planList(applyConditions(p.Filter, []PlanCondition{c})).Total > 0 {
			out = append(out, c)
		}
	}
	return out
}

// drawFilter draws n conditions to add to the list p: the kinds by the weights of FilterKinds, each
// kind once, and of a kind any value it offers alike. Each condition is drawn on the list the ones
// before it leave, so the list after all of them shows a product. It reports false when the list
// offers fewer than n kinds.
func drawFilter(rng *rand.Rand, p ListPage, n int) ([]PlanCondition, bool) {
	var conds []PlanCondition
	cur := p
	for len(conds) < n {
		var kinds [][]PlanCondition
		var weights []float64
		var total float64
		for _, k := range FilterKinds {
			if slices.ContainsFunc(conds, func(c PlanCondition) bool { return c.Kind == k.Kind }) {
				continue
			}
			if opts := filterOptions(cur, k.Kind); len(opts) > 0 {
				kinds, weights, total = append(kinds, opts), append(weights, k.Weight), total+k.Weight
			}
		}
		if len(kinds) == 0 {
			return nil, false
		}
		at := len(kinds) - 1
		for x, i := rng.Float64()*total, 0; i < len(kinds); i++ {
			if x -= weights[i]; x < 0 {
				at = i
				break
			}
		}
		conds = append(conds, kinds[at][rng.IntN(len(kinds[at]))])
		cur = planList(applyConditions(p.Filter, conds))
	}
	return conds, true
}

// filterStep is the page view of a filter that adds conds to the list p. One condition that a plain
// link of the page adds (a category of the category bar, a quick filter, a price band) is a click on
// that link, a document load. Any other, and two or three at once, go through the form of the sidebar,
// which the router takes.
func filterStep(p ListPage, conds []PlanCondition) PlanStep {
	s := PlanStep{Kind: StepClick, Page: applyConditions(p.Filter, conds), Act: ActForm, Set: setOf(conds)}
	if len(conds) == 1 {
		for _, l := range p.Links() {
			if l.Plain && l.To == s.Page && (l.Part == ListPartCategory || l.Part == ListPartQuick || l.Part == ListPartBand) {
				s.Act, s.Part = ActFilter, l.Part
				break
			}
		}
	}
	return s
}

// sortOptions are the sorts a visitor changes the list p to: the four that are not the default, but
// the one the list has, on a list with two products or more.
func sortOptions(p ListPage) []string {
	if p.Total < 2 {
		return nil
	}
	var out []string
	for _, s := range listSorts {
		if s != "featured" && s != p.Filter.Sort {
			out = append(out, s)
		}
	}
	return out
}

// sortStep is the page view of the list p sorted another way: the sort box takes the visitor to the
// first page of the list with that sort (Toolbar in page.tsx), through the router.
func sortStep(p ListPage, sort string) PlanStep {
	v := p.Filter.values()
	v.sort = sort
	return PlanStep{Kind: StepClick, Page: v.href(p.Filter.Lang, 1), Act: ActSort, Sort: sort}
}

// pagesAhead is how many pages the list p has after the one it shows, for a visitor who pages: 0 for
// a list with a search term, which the paging figure is not used on (ListPageShares).
func pagesAhead(p ListPage) int {
	if strings.TrimSpace(p.Filter.Query) != "" {
		return 0
	}
	return p.Pages - p.Filter.Page
}

// pageStep is the page view of the next page of the list p, by a link of the pages of the list, a
// plain link.
func pageStep(p ListPage) PlanStep {
	return PlanStep{Kind: StepClick, Page: p.Filter.values().href(p.Filter.Lang, p.Filter.Page+1), Act: ActPage, Part: ListPartPage}
}

// isList says whether a page of a plan is a product list.
func isList(page string) bool {
	_, kind := StorePageKind(page)
	return kind == StoreKindList
}

// draftVisit is a shopper visit while its block is planned: its plan, the page view of its search (-1
// for none) and the class of its word, the last page view a write follows (0 for none), and whether
// it has filtered, sorted or paged.
type draftVisit struct {
	plan   *Plan
	search int
	class  string
	locked int
	acted  bool
}

// rewalk draws the page views of a visit again from the page view from on, each a click on a link of
// the page before, and keeps its search where it is.
func (d *draftVisit) rewalk(rng *rand.Rand, from int) {
	steps := d.plan.Steps
	for j := max(from, 1); j < len(steps); j++ {
		if j != d.search {
			steps[j] = linkStep(pick(rng, StoreLinks(steps[j-1].Page)))
		}
	}
}

// spots are the page views of a visit an action on a list can take the place of: after a list the
// action can be taken on, after the page views the writes follow, not the search, and with room page
// views after it that are not the search either.
func (d *draftVisit) spots(room int, can func(ListPage) bool) []int {
	var out []int
	steps := d.plan.Steps
	for j := d.locked + 1; j+room < len(steps); j++ {
		if j == d.search || room > 0 && j+room == d.search || !isList(steps[j-1].Page) || !can(planList(steps[j-1].Page)) {
			continue
		}
		out = append(out, j)
	}
	return out
}

// planListActions deals the filters, the sorts and the pages of the shopper visits of a block (steps
// 5 to 7 above), and fails when the visits cannot take a quota.
func planListActions(rng *rand.Rand, visits []*draftVisit) error {
	lists := 0
	for _, d := range visits {
		if slices.ContainsFunc(d.plan.Steps, func(s PlanStep) bool { return isList(s.Page) }) {
			lists++
		}
	}
	order := rng.Perm(len(visits))
	// take gives the action to the first visit of the shuffle that has not acted and has a spot for it,
	// at a spot drawn alike among them, and walks the page views after it again.
	take := func(what string, room int, can func(ListPage) bool, act func(d *draftVisit, at int, p ListPage) bool) error {
		for _, i := range order {
			d := visits[i]
			if d.acted {
				continue
			}
			spots := d.spots(room, can)
			rng.Shuffle(len(spots), func(a, b int) { spots[a], spots[b] = spots[b], spots[a] })
			for _, at := range spots {
				if act(d, at, planList(d.plan.Steps[at-1].Page)) {
					d.acted = true
					d.rewalk(rng, at+1+room)
					return nil
				}
			}
		}
		return fmt.Errorf("no visit is left that can %s", what)
	}
	// The filters, those with the most conditions first, which the fewest lists can take.
	filters := quotaCounts(int(math.Round(ListFilterShare*float64(lists))), FilterConditionShares)
	for n := len(filters); n >= 1; n-- {
		for range filters[n-1] {
			err := take(fmt.Sprintf("filter a list by %d conditions", n), 0, func(ListPage) bool { return true }, func(d *draftVisit, at int, p ListPage) bool {
				conds, ok := drawFilter(rng, p, n)
				if ok {
					d.plan.Steps[at] = filterStep(p, conds)
				}
				return ok
			})
			if err != nil {
				return err
			}
		}
	}
	for range int(math.Round(ListSortShare * float64(lists))) {
		err := take("sort a list", 0, func(p ListPage) bool { return len(sortOptions(p)) > 0 }, func(d *draftVisit, at int, p ListPage) bool {
			sorts := sortOptions(p)
			d.plan.Steps[at] = sortStep(p, sorts[rng.IntN(len(sorts))])
			return true
		})
		if err != nil {
			return err
		}
	}
	// The pages, counted on the plans as the filters and the sorts left them.
	paged := 0
	for _, d := range visits {
		if seesPagedList(d.plan.Steps) {
			paged++
		}
	}
	for range int(math.Round(ListPageShares[2] * float64(paged))) {
		err := take("go to the third page of a list", 1, func(p ListPage) bool { return pagesAhead(p) >= 2 }, func(d *draftVisit, at int, p ListPage) bool {
			d.plan.Steps[at] = pageStep(p)
			d.plan.Steps[at+1] = pageStep(planList(d.plan.Steps[at].Page))
			return true
		})
		if err != nil {
			return err
		}
	}
	for range int(math.Round(ListPageShares[1] * float64(paged))) {
		err := take("go to the second page of a list", 0, func(p ListPage) bool { return pagesAhead(p) >= 1 }, func(d *draftVisit, at int, p ListPage) bool {
			d.plan.Steps[at] = pageStep(p)
			return true
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// seesPagedList says whether a visit sees a product list of more than one page with no search term,
// at any of its pages.
func seesPagedList(steps []PlanStep) bool {
	return slices.ContainsFunc(steps, func(s PlanStep) bool {
		if !isList(s.Page) {
			return false
		}
		p := planList(s.Page)
		return strings.TrimSpace(p.Filter.Query) == "" && p.Pages > 1
	})
}

// termClass is the class of the word of a search step.
func termClass(term string) string {
	switch {
	case term == SearchPlaceholder:
		return TermUnique
	case termFinds(term) == 0:
		return TermNone
	}
	return TermFound
}

// variantOf is the visit of a plan with another word in its search, at page view at: the page views
// before it as they are, and each one after it reached by the same act from the page the variant is
// on. A click on a link leads to a page of the same kind, the same page when the variant's page links
// to it and it is no product, and else one drawn among the links of that kind. A filter adds the same
// conditions, a sort sets the same sort, a page is the next page. It reports false when the pages of
// the word do not allow one of them, and the caller draws another word.
func variantOf(rng *rand.Rand, p Plan, at int, term string) ([]PlanStep, bool) {
	lang, _ := StorePageKind(p.Steps[0].Page)
	out := slices.Clone(p.Steps)
	out[at] = searchStep(lang, term)
	for j := at + 1; j < len(out); j++ {
		base, prev := p.Steps[j], out[j-1].Page
		if base.Act == ActLink {
			_, kind := StorePageKind(base.Page)
			var links []Link
			var sum float64
			same := false
			for _, l := range StoreLinks(prev) {
				if _, k := StorePageKind(l.To); k == kind {
					links, sum = append(links, l), sum+l.Weight
					same = same || l.To == base.Page
				}
			}
			switch {
			case len(links) == 0:
				return nil, false
			case same && kind != StoreKindProduct:
				out[j] = linkStep(base.Page)
			default:
				for i := range links {
					links[i].Weight /= sum
				}
				out[j] = linkStep(pick(rng, links))
			}
			continue
		}
		if !isList(prev) {
			return nil, false
		}
		list := planList(prev)
		var step PlanStep
		switch base.Act {
		case ActFilter, ActForm:
			conds := conditionsOf(base.Set)
			cur := list
			for i, c := range conds {
				if !slices.Contains(filterOptions(cur, c.Kind), c) {
					return nil, false
				}
				cur = planList(applyConditions(list.Filter, conds[:i+1]))
			}
			step = filterStep(list, conds)
		case ActSort:
			if !slices.Contains(sortOptions(list), base.Sort) {
				return nil, false
			}
			step = sortStep(list, base.Sort)
		case ActPage:
			if pagesAhead(list) < 1 {
				return nil, false
			}
			step = pageStep(list)
		default:
			return nil, false
		}
		if step.Act != base.Act || step.Part != base.Part {
			return nil, false
		}
		out[j] = step
	}
	return out, true
}

// planVariants gives the visits of a block that search for a word of the list their variants: for
// each, PlanVariants-1 other words of the same class, drawn as the word of the visit was, so a word
// can come twice and the words of the variants follow the same Zipf law.
func planVariants(seed uint64, b int, visits []*draftVisit) error {
	for i, d := range visits {
		if d.search < 0 || d.class == TermUnique {
			continue
		}
		lang, _ := StorePageKind(d.plan.Steps[0].Page)
		rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(b)<<20|uint64(i)))
		for len(d.plan.Variants) < PlanVariants-1 {
			found := false
			for range 1000 {
				if v, ok := variantOf(rng, *d.plan, d.search, drawSearchTerm(rng, lang, d.class)); ok {
					d.plan.Variants, found = append(d.plan.Variants, v), true
					break
				}
			}
			if !found {
				return fmt.Errorf("visit %d found no other word its pages after the search allow", i)
			}
		}
	}
	return nil
}

// ActionCounts are what shopper visits do with the product list, counted from their plans: the
// searches, of them those that find nothing and those for a word of the visit's own, the visits that
// see a list, of them those that filter and those that sort, the filters by the conditions they set
// (one, two, three), the visits that see a list of more than one page with no search term, and of them
// those that go to the second page and no further and those that go on to the third.
type ActionCounts struct {
	Searches       int    `json:"searches"`
	SearchesNone   int    `json:"searchesWithNoResult"`
	SearchesUnique int    `json:"searchesForAWordOfTheirOwn"`
	ListVisits     int    `json:"visitsThatSeeAList"`
	Filters        int    `json:"filters"`
	Conditions     [3]int `json:"filtersByConditions"`
	Sorts          int    `json:"sorts"`
	PagedVisits    int    `json:"visitsThatSeeAListOfSeveralPages"`
	SecondPages    int    `json:"visitsToTheSecondPage"`
	ThirdPages     int    `json:"visitsToTheThirdPage"`
}

// Add counts a shopper plan. The word of a search, and the pages of a list, are what the model of the
// list says of them.
func (c *ActionCounts) Add(p Plan) {
	if p.Role != RoleShopper {
		return
	}
	pages, list := 0, false
	for i, s := range p.Steps {
		list = list || isList(s.Page)
		if i == 0 {
			continue
		}
		switch s.Act {
		case ActSearch:
			c.Searches++
			switch termClass(s.Term) {
			case TermNone:
				c.SearchesNone++
			case TermUnique:
				c.SearchesNone++
				c.SearchesUnique++
			}
		case ActFilter, ActForm:
			c.Filters++
			if n := len(conditionsOf(s.Set)); n >= 1 && n <= len(c.Conditions) {
				c.Conditions[n-1]++
			}
		case ActSort:
			c.Sorts++
		case ActPage:
			pages++
		}
	}
	if list {
		c.ListVisits++
	}
	if seesPagedList(p.Steps) {
		c.PagedVisits++
	}
	switch {
	case pages == 1:
		c.SecondPages++
	case pages > 1:
		c.ThirdPages++
	}
}

// quotas says how the counts are not the quotas of the mix for whole blocks, exactly, or "".
func (c ActionCounts) quotas(blocks int) string {
	round := func(x float64) int { return int(math.Round(x)) }
	searches := blocks * round(SearchShare*ShopperBlockVisits)
	classes := quotaCounts(round(SearchShare*ShopperBlockVisits), []float64{1 - SearchNoResultShare, SearchNoResultShare - SearchUniqueShare, SearchUniqueShare})
	switch {
	case c.Searches != searches:
		return fmt.Sprintf("%d visits search, the mix has %d", c.Searches, searches)
	case c.SearchesNone != blocks*(classes[1]+classes[2]) || c.SearchesUnique != blocks*classes[2]:
		return fmt.Sprintf("%d searches find nothing, %d of them for a word of their own, the mix has %d and %d", c.SearchesNone, c.SearchesUnique, blocks*(classes[1]+classes[2]), blocks*classes[2])
	}
	if blocks != 1 {
		return ""
	}
	// The quotas that are shares of a count of the block itself are exact for one block.
	filters := round(ListFilterShare * float64(c.ListVisits))
	conds := quotaCounts(filters, FilterConditionShares)
	switch {
	case c.Filters != filters || c.Conditions != [3]int{conds[0], conds[1], conds[2]}:
		return fmt.Sprintf("of %d visits that see a list %d filter it, with %v conditions, the mix has %d with %v", c.ListVisits, c.Filters, c.Conditions, filters, conds)
	case c.Sorts != round(ListSortShare*float64(c.ListVisits)):
		return fmt.Sprintf("of %d visits that see a list %d change the sort, the mix has %d", c.ListVisits, c.Sorts, round(ListSortShare*float64(c.ListVisits)))
	case c.SecondPages != round(ListPageShares[1]*float64(c.PagedVisits)) || c.ThirdPages != round(ListPageShares[2]*float64(c.PagedVisits)):
		return fmt.Sprintf("of %d visits that see a list of several pages %d go to the second page and %d to the third, the mix has %d and %d", c.PagedVisits, c.SecondPages, c.ThirdPages,
			round(ListPageShares[1]*float64(c.PagedVisits)), round(ListPageShares[2]*float64(c.PagedVisits)))
	}
	return ""
}

// checkActionQuotas checks, from the plans themselves, that every page view after a landing says what
// the visitor does, and that the shopper plans of each block search, filter, sort and page by the
// quotas of the mix. Plans made before the searches have no acts.
func checkActionQuotas(set *PlanSet) error {
	counts := make([]ActionCounts, set.Blocks)
	for _, p := range set.Plans {
		for j, s := range p.Steps {
			switch {
			case j == 0 && (s.Kind != StepLoad || s.Act != ""):
				return fmt.Errorf("plan %d does not start with a landing", p.ID)
			case j == 0:
			case s.Kind != StepClick || !slices.Contains([]string{ActLink, ActSearch, ActFilter, ActForm, ActSort, ActPage}, s.Act):
				return fmt.Errorf("plan %d: page view %d does not say what the visitor does on the page before it (the act %q)", p.ID, j, s.Act)
			case p.Role != RoleShopper && s.Act != ActLink:
				return fmt.Errorf("plan %d of the staff: page view %d is a %s", p.ID, j, s.Act)
			}
		}
		if p.Block >= 0 && p.Block < set.Blocks {
			counts[p.Block].Add(p)
		}
	}
	for b, c := range counts {
		if why := c.quotas(1); why != "" {
			return fmt.Errorf("block %d: %s", b, why)
		}
	}
	return nil
}

// checkVariants checks that every plan that searches for a word of the list has its variants, and that
// each is the visit with another word of the same class: as many page views, the same before the
// search, and after it each of the same kind of page, by the same act with the same part, conditions
// and sort. A plan that does not search for a word of the list has none.
func checkVariants(set *PlanSet) error {
	for _, p := range set.Plans {
		at := slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Act == ActSearch })
		if at < 0 || p.Steps[at].Term == SearchPlaceholder {
			if len(p.Variants) != 0 {
				return fmt.Errorf("plan %d has %d variants and no search for a word of the list", p.ID, len(p.Variants))
			}
			continue
		}
		if len(p.Variants) != PlanVariants-1 {
			return fmt.Errorf("plan %d searches for a word of the list and has %d variants, not %d", p.ID, len(p.Variants), PlanVariants-1)
		}
		class := termClass(p.Steps[at].Term)
		for n, v := range p.Variants {
			if len(v) != len(p.Steps) || !slices.Equal(v[:at], p.Steps[:at]) {
				return fmt.Errorf("plan %d: variant %d has %d page views for %d, or differs before the search", p.ID, n+1, len(v), len(p.Steps))
			}
			if v[at].Act != ActSearch || termClass(v[at].Term) != class || v[at].Page != searchStep(langOf(p.Steps[0].Page), v[at].Term).Page {
				return fmt.Errorf("plan %d: variant %d searches for %q, which is not a word of the class of %q", p.ID, n+1, v[at].Term, p.Steps[at].Term)
			}
			for j := at + 1; j < len(v); j++ {
				a, b := p.Steps[j], v[j]
				_, ka := StorePageKind(a.Page)
				_, kb := StorePageKind(b.Page)
				if a.Kind != b.Kind || a.Act != b.Act || a.Part != b.Part || a.Set != b.Set || a.Sort != b.Sort || ka != kb || langOf(a.Page) != langOf(b.Page) {
					return fmt.Errorf("plan %d: page view %d of variant %d is not the one of the visit with another word", p.ID, j, n+1)
				}
			}
		}
	}
	return nil
}

// langOf is the language of a page of the storefront.
func langOf(page string) string {
	lang, _ := StorePageKind(page)
	return lang
}
