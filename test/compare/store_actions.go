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
//     quota and the word drawn in the class (store_terms.go), with the words of the variants of the
//     visit, each drawn on its own (dealSearches). The search box is in the header of every page, so a
//     search can follow any page.
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
//
// A visit that searches for a word of the list has PlanVariants forms, the visit and its variants,
// which differ in the word. Their words are dealt in step 2 and never change: from step 3 on the
// forms are walked together (draftVisit), and what follows the search is what the lists of all the
// words allow. A filter or a sort that one of them does not allow is given to another visit.
//
// A filter, a sort or a page is an action on a list the visit is on: it takes the place of the page view
// after that list, and the page views after it are walked again from the page it leads to, the search
// of the visit staying where it is. So that the quotas come out exact, a visit does one of the three at
// most (ListActionsPerVisit, an assumption: nothing was found on how they go together, and it makes
// the share of the visits that sort or page after a filter 0), and the action comes after the page
// views its writes follow, which stay as they are. Which visits see a list does not change from
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

// drawFilter draws n conditions to add to the lists, which are the lists of the forms of one visit (one
// list for a visit with no variants): the kinds by the weights of FilterKinds, each kind once, and of a
// kind any value alike that every one of the lists offers. Each condition is drawn on the lists the
// ones before it leave, so every list shows a product after all of them. It reports false when the
// lists offer fewer than n kinds in common.
func drawFilter(rng *rand.Rand, lists []ListPage, n int) ([]PlanCondition, bool) {
	var conds []PlanCondition
	cur := slices.Clone(lists)
	for len(conds) < n {
		var kinds [][]PlanCondition
		var weights []float64
		var total float64
		for _, k := range FilterKinds {
			if slices.ContainsFunc(conds, func(c PlanCondition) bool { return c.Kind == k.Kind }) {
				continue
			}
			opts := filterOptions(cur[0], k.Kind)
			for _, p := range cur[1:] {
				other := filterOptions(p, k.Kind)
				opts = slices.DeleteFunc(slices.Clone(opts), func(c PlanCondition) bool { return !slices.Contains(other, c) })
			}
			if len(opts) > 0 {
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
		for v, p := range lists {
			cur[v] = planList(applyConditions(p.Filter, conds))
		}
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

// searchDeal is the search a shopper visit is dealt: the page view it searches at (0 for a visit that
// does not search), the class of its words, and the words: the one of the visit and then those of its
// variants, PlanVariants in all, or the placeholder alone for a word of the visit's own.
type searchDeal struct {
	At    int
	Class string
	Terms []string
}

// dealSearches deals the searches of the shopper visits of block b (step 2 above), from the lengths
// and the languages of the visits alone and in a shuffle of its own: which visits search, at which
// page view, for words of which class, and the words, each drawn on its own (drawSearchTerm). Nothing
// that is planned later changes a word: what follows a search is fitted to the words the visit has.
func dealSearches(seed uint64, b int, lens []int, langs []string) []searchDeal {
	out := make([]searchDeal, len(lens))
	rng := rand.New(rand.NewPCG(seed^0x510e527fade682d1, uint64(b)))
	classes := searchClasses(rng, int(math.Round(SearchShare*float64(len(lens)))))
	for _, i := range rng.Perm(len(lens)) {
		if len(classes) == 0 || lens[i] < 2 {
			continue
		}
		deal := searchDeal{At: 1 + rng.IntN(lens[i]-1), Class: classes[0]}
		words := PlanVariants
		if deal.Class == TermUnique {
			words = 1
		}
		for range words {
			deal.Terms = append(deal.Terms, drawSearchTerm(rng, langs[i], deal.Class))
		}
		out[i], classes = deal, classes[1:]
	}
	if len(classes) > 0 {
		panic(fmt.Sprintf("block %d of seed %d: %d searches found no visit of two page views", b, seed, len(classes)))
	}
	return out
}

// draftVisit is a shopper visit while its block is planned: its plan, the page view of its search (-1
// for none) and the words it was dealt, the last page view a write follows (0 for none), and how many
// times it has filtered, sorted or paged (ListActionsPerVisit at most).
//
// A visit that searches for a word of the list is planned in all its forms at once, the visit and its
// variants: paths holds the page views of each, the Steps of the plan first. The forms have the same
// page views up to the search, each its own word at the search, and after it they are walked
// together: a page view after the search is one every form can make from the page it is on, a click
// to a page of the same kind or the same action on its list. So a word is never given up for what a
// later page view needs. A filter or a sort that the list of one of the words does not allow is not
// given to this visit after its search, and goes to another visit. Any other visit has one path.
type draftVisit struct {
	plan   *Plan
	paths  [][]PlanStep
	search int
	terms  []string
	locked int
	acted  int
}

// newDraft is the draft of a plan with its landing and the search it was dealt.
func newDraft(p *Plan, deal searchDeal) *draftVisit {
	d := &draftVisit{plan: p, paths: [][]PlanStep{p.Steps}, search: -1}
	if deal.At > 0 {
		d.search, d.terms = deal.At, deal.Terms
		for len(d.paths) < len(deal.Terms) {
			d.paths = append(d.paths, slices.Clone(p.Steps))
		}
	}
	return d
}

// set gives page view j of every form its step.
func (d *draftVisit) set(j int, steps []PlanStep) {
	for v, path := range d.paths {
		path[j] = steps[v]
	}
}

// keep is a copy of the page views of every form, and put brings one back.
func (d *draftVisit) keep() [][]PlanStep {
	out := make([][]PlanStep, len(d.paths))
	for v, path := range d.paths {
		out[v] = slices.Clone(path)
	}
	return out
}

func (d *draftVisit) put(kept [][]PlanStep) {
	for v, path := range d.paths {
		copy(path, kept[v])
	}
}

// rewalk draws the page views of a visit again from the page view from on, each a click on a link of
// the page before, and keeps its search where it is. Before the search every form gets the same click.
// After it the forms click together (clickTogether).
func (d *draftVisit) rewalk(rng *rand.Rand, from int) {
	lang := langOf(d.paths[0][0].Page)
	for j := max(from, 1); j < len(d.paths[0]); j++ {
		switch {
		case j == d.search:
			for v, path := range d.paths {
				path[j] = searchStep(lang, d.terms[v])
			}
		case j < d.search || len(d.paths) == 1:
			step := linkStep(pick(rng, StoreLinks(d.paths[0][j-1].Page)))
			for _, path := range d.paths {
				path[j] = step
			}
		default:
			d.clickTogether(rng, j)
		}
	}
}

// clickTogether makes page view j of every form a click on a link of the page that form is on, all to
// a page of the same kind: the kind is drawn by the weights of the links of the visit itself among
// the kinds every form has a link to, which is how a single visit draws it when all have them. The
// visit then draws its page among its links of the kind by their weights, and so does each variant
// among its own, but for a page that is no product, where a variant takes the page of the visit when
// it has a link to it: the same list, the home, the cart.
func (d *draftVisit) clickTogether(rng *rand.Rand, j int) {
	type group struct {
		links []Link
		sum   float64
	}
	by := make([]map[string]*group, len(d.paths))
	for v, path := range d.paths {
		by[v] = map[string]*group{}
		for _, l := range StoreLinks(path[j-1].Page) {
			_, kind := StorePageKind(l.To)
			g := by[v][kind]
			if g == nil {
				g = &group{}
				by[v][kind] = g
			}
			g.links, g.sum = append(g.links, l), g.sum+l.Weight
		}
	}
	var kinds []string
	var total float64
	for _, l := range StoreLinks(d.paths[0][j-1].Page) {
		_, kind := StorePageKind(l.To)
		if slices.Contains(kinds, kind) || slices.ContainsFunc(by, func(m map[string]*group) bool { return m[kind] == nil }) {
			continue
		}
		kinds, total = append(kinds, kind), total+by[0][kind].sum
	}
	if len(kinds) == 0 {
		panic(fmt.Sprintf("plan %d: the forms of the visit have no kind of link in common after %s", d.plan.ID, d.paths[0][j-1].Page))
	}
	kind := kinds[len(kinds)-1]
	for x, i := rng.Float64()*total, 0; i < len(kinds); i++ {
		if x -= by[0][kinds[i]].sum; x < 0 {
			kind = kinds[i]
			break
		}
	}
	draw := func(g *group) string {
		links := slices.Clone(g.links)
		for i := range links {
			links[i].Weight /= g.sum
		}
		return pick(rng, links)
	}
	page := draw(by[0][kind])
	for v, path := range d.paths {
		switch g := by[v][kind]; {
		case v == 0, kind != StoreKindProduct && slices.ContainsFunc(g.links, func(l Link) bool { return l.To == page }):
			path[j] = linkStep(page)
		default:
			path[j] = linkStep(draw(g))
		}
	}
}

// listsAt are the lists the forms of a visit are on at page view j, nil when one of them is on a page
// that is no list.
func (d *draftVisit) listsAt(j int) []ListPage {
	out := make([]ListPage, len(d.paths))
	for v, path := range d.paths {
		if !isList(path[j].Page) {
			return nil
		}
		out[v] = planList(path[j].Page)
	}
	return out
}

// spots are the page views of a visit an action on a list can take the place of: after a list of
// every form that the action can be taken on, after the page views the writes follow, not the search,
// and with room page views after it that are not the search either.
func (d *draftVisit) spots(room int, can func(ListPage) bool) []int {
	var out []int
	for j := d.locked + 1; j+room < len(d.paths[0]); j++ {
		if j == d.search || room > 0 && j+room == d.search {
			continue
		}
		if lists := d.listsAt(j - 1); lists != nil && !slices.ContainsFunc(lists, func(p ListPage) bool { return !can(p) }) {
			out = append(out, j)
		}
	}
	return out
}

// finish gives the plan its variants: the other forms of the visit, each with the page views of the
// visit before the search, the landing with its parameter among them.
func (d *draftVisit) finish() {
	for _, path := range d.paths[1:] {
		copy(path[:d.search], d.paths[0][:d.search])
		d.plan.Variants = append(d.plan.Variants, path)
	}
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
	// at a spot drawn alike among them, and walks the page views after it again. The action is taken on
	// the list of every form of the visit, and act reports false when one of them does not allow it.
	take := func(what string, room int, who func(*draftVisit) bool, can func(ListPage) bool, act func(d *draftVisit, at int, lists []ListPage) bool) error {
		for _, i := range order {
			d := visits[i]
			if d.acted >= ListActionsPerVisit || !who(d) {
				continue
			}
			spots := d.spots(room, can)
			rng.Shuffle(len(spots), func(a, b int) { spots[a], spots[b] = spots[b], spots[a] })
			for _, at := range spots {
				if act(d, at, d.listsAt(at-1)) {
					d.acted++
					d.rewalk(rng, at+1+room)
					return nil
				}
			}
		}
		return fmt.Errorf("no visit is left that can %s", what)
	}
	// each makes the step of every form from its list.
	each := func(lists []ListPage, step func(ListPage) PlanStep) []PlanStep {
		out := make([]PlanStep, len(lists))
		for v, p := range lists {
			out[v] = step(p)
		}
		return out
	}
	// The filters, those with the most conditions first, which the fewest lists can take.
	filters := quotaCounts(int(math.Round(ListFilterShare*float64(lists))), FilterConditionShares)
	for n := len(filters); n >= 1; n-- {
		for range filters[n-1] {
			err := take(fmt.Sprintf("filter a list by %d conditions", n), 0, anyVisit, func(ListPage) bool { return true }, func(d *draftVisit, at int, lists []ListPage) bool {
				conds, ok := drawFilter(rng, lists, n)
				if !ok {
					return false
				}
				steps := each(lists, func(p ListPage) PlanStep { return filterStep(p, conds) })
				// The same control on every form: the plain link of the same part, or the form.
				if slices.ContainsFunc(steps, func(s PlanStep) bool { return s.Act != steps[0].Act || s.Part != steps[0].Part }) {
					return false
				}
				d.set(at, steps)
				return true
			})
			if err != nil {
				return err
			}
		}
	}
	for range int(math.Round(ListSortShare * float64(lists))) {
		err := take("sort a list", 0, anyVisit, func(p ListPage) bool { return len(sortOptions(p)) > 0 }, func(d *draftVisit, at int, lists []ListPage) bool {
			// A sort every form can change to.
			var sorts []string
			for _, s := range sortOptions(lists[0]) {
				if !slices.ContainsFunc(lists, func(p ListPage) bool { return !slices.Contains(sortOptions(p), s) }) {
					sorts = append(sorts, s)
				}
			}
			if len(sorts) == 0 {
				return false
			}
			sort := sorts[rng.IntN(len(sorts))]
			d.set(at, each(lists, func(p ListPage) PlanStep { return sortStep(p, sort) }))
			return true
		})
		if err != nil {
			return err
		}
	}
	// The pages, counted on the plans as the filters and the sorts left them, by the deepest page each
	// visit sees. A visit that lands on a second page has seen it, so the visits that are that deep
	// already count toward the quotas, and the page actions fill what is missing: two of them, from a
	// first page, for a visit that is to see the third page, and one for a visit that is to stop at the
	// second. They go to visits that have seen first pages alone, so a visit that landed deep stays where
	// it is counted. When more visits land on a second page than the quota, none is taken away, and no
	// page action is added.
	var have ActionCounts
	for _, d := range visits {
		have.Add(*d.plan)
	}
	second, third := have.pageQuotas()
	shallow := func(d *draftVisit) bool { return listDepth(d.plan.Steps) == 1 }
	first := func(ahead int) func(ListPage) bool {
		return func(p ListPage) bool { return p.Filter.Page == 1 && pagesAhead(p) >= ahead }
	}
	for range third - have.ThirdPages {
		err := take("go to the third page of a list", 1, shallow, first(2), func(d *draftVisit, at int, lists []ListPage) bool {
			steps := each(lists, pageStep)
			d.set(at, steps)
			d.set(at+1, each(lists, func(p ListPage) PlanStep { return pageStep(planList(pageStep(p).Page)) }))
			return true
		})
		if err != nil {
			return err
		}
	}
	for range second - have.SecondPages {
		err := take("go to the second page of a list", 0, shallow, first(1), func(d *draftVisit, at int, lists []ListPage) bool {
			d.set(at, each(lists, pageStep))
			return true
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// anyVisit lets every visit take an action.
func anyVisit(*draftVisit) bool { return true }

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
	// SecondPages are the visits whose deepest page of such a list is the second, and ThirdPages those
	// that see the third or a deeper one, however they got there. SecondPagesLanded and ThirdPagesLanded
	// are those of them that took no page action: they landed on that page.
	SecondPages       int `json:"visitsToTheSecondPage"`
	ThirdPages        int `json:"visitsToTheThirdPage"`
	SecondPagesLanded int `json:"visitsThatLandOnASecondPage"`
	ThirdPagesLanded  int `json:"visitsThatLandOnAThirdPage"`
}

// listDepth is the deepest page a visit sees of a product list of more than one page with no search
// term: 0 when it sees no such list, 1 when it stays on first pages. It is what the visit saw, not how
// many times it went to a next page: a visit that lands on a second page has seen the second page.
func listDepth(steps []PlanStep) int {
	deepest := 0
	for _, s := range steps {
		if isList(s.Page) {
			if p := planList(s.Page); strings.TrimSpace(p.Filter.Query) == "" && p.Pages > 1 {
				deepest = max(deepest, p.Filter.Page)
			}
		}
	}
	return deepest
}

// pageQuotas are how many of the visits that see a list of several pages stop at the second page and
// how many see the third or deeper, by ListPageShares: the share of the visits, or the visits that
// land that deep when they are more, since a landing is not taken away to make a quota.
func (c ActionCounts) pageQuotas() (second, third int) {
	second = max(int(math.Round(ListPageShares[1]*float64(c.PagedVisits))), c.SecondPagesLanded)
	third = max(int(math.Round(ListPageShares[2]*float64(c.PagedVisits))), c.ThirdPagesLanded)
	return second, third
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
	switch depth := listDepth(p.Steps); {
	case depth == 0:
	case depth == 1:
		c.PagedVisits++
	case depth == 2:
		c.PagedVisits++
		c.SecondPages++
		if pages == 0 {
			c.SecondPagesLanded++
		}
	default:
		c.PagedVisits++
		c.ThirdPages++
		if pages == 0 {
			c.ThirdPagesLanded++
		}
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
	}
	if second, third := c.pageQuotas(); c.SecondPages != second || c.ThirdPages != third {
		return fmt.Sprintf("of %d visits that see a list of several pages %d see the second page and no deeper (%d land there) and %d the third or deeper (%d land there), the mix has %d and %d",
			c.PagedVisits, c.SecondPages, c.SecondPagesLanded, c.ThirdPages, c.ThirdPagesLanded, second, third)
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
