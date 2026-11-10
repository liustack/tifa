package compare

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"testing"
)

// staffPages are pages of the back office the check follows with the pages of the storefront.
var staffPages = []string{"/projects/studio", "/projects/studio/stats", "/projects/studio/orders/SH-1042", "/projects/weekend"}

// versionStep is one write of the check of the data versions: what it sends, and the effects the model
// takes for it.
type versionStep struct {
	name    string
	kind    string // the kind of write, which the counts are summed by
	effects []effect
	do      func()
	// same says that the write sets the value the product has already: the model changes the version and
	// the data stays, which is counted apart.
	same bool
	// changes are pages whose data the write must change, or the write is not the case it is here for.
	changes []string
}

// versionTally counts the (page, write) pairs of the check: those the model says changed, of them those
// whose data did not, and those whose data changed and the model says did not.
type versionTally struct{ changed, vain, missed int }

func (c versionTally) share() float64 { return 100 * float64(c.vain) / float64(max(c.changed, 1)) }

// versionLists are the URLs of the product list the check of the data versions asks for, each once, and
// how many there are of each source. The model works out every list, so the check takes the lists the
// run asks for and more: the lists a visit can land on and those of the crawlers (which the file had
// before the model took the lists over), the search for every word of the word lists in both
// languages and for a few words of a visit's own, every list a plan of the first block of the formal
// seed is on in any of its forms (the lists after a search, the filters by a link, the forms, the
// sorts, the pages), and 200 lists drawn from a seed (listSample), which have what the plans have not:
// several values of a filter, values the shop does not have, ranges typed by hand, pages past the end.
func versionLists() (urls []string, counts []string) {
	seen := map[string]bool{}
	add := func(what string, list []string) {
		n := 0
		for _, u := range list {
			if u = stripLanding(u); !seen[u] {
				seen[u] = true
				urls = append(urls, u)
				n++
			}
		}
		counts = append(counts, fmt.Sprintf("%d %s", n, what))
	}
	var fixed, words, own, planned []string
	public, _ := CrawlURLs()
	for _, u := range slices.Concat(StoreVisitPages(), public) {
		if _, kind := StorePageKind(u); kind == StoreKindList {
			fixed = append(fixed, u)
		}
	}
	for _, l := range StoreLangs {
		terms := StoreSearchTerms()[l]
		for _, term := range slices.Concat(terms.Found, terms.None) {
			words = append(words, searchStep(l, term).Page)
		}
		for seq := range 5 {
			own = append(own, searchStep(l, uniqueTerm(FormalBrowse.Seed, seq)).Page)
		}
	}
	for _, p := range GenerateSitePlanSet(FormalBrowse.Seed, 1).Plans {
		for v := range 1 + len(p.Variants) {
			for _, s := range planForm(p, v).Steps {
				if _, kind := StorePageKind(s.Page); kind == StoreKindList && !strings.Contains(s.Page, SearchPlaceholder) {
					planned = append(planned, s.Page)
				}
			}
		}
	}
	add("lists of the landings and of the crawlers", fixed)
	add("searches for a word of the word lists", words)
	add("searches for a word of a visit's own", own)
	add("more lists of the plans of a block", planned)
	add("more lists drawn from a seed", listSample(3, 200))
	return urls, counts
}

// TestTheDataVersionsFollowTheShop makes the writes of the mix on a shop that runs, several of each
// kind and in sequences where one write decides what the next one changes, and asks every page of
// store_deps.json, the lists of versionLists and a few pages of the back office for its data after
// each. The data version model (versionModel, with the effects of the writes, store_deps.json and the
// list model) says which pages each write changes.
//
//   - A page the model says is unchanged must have the same data, and two requests of a page with one
//     version must have the same data however many writes lie between them. Otherwise the model misses a
//     dependency and the expected hit rate would come out too high: that fails.
//   - The products the model works out for a part of a page that a price or a stock decides (the sale of
//     the home, a list by price, the best rated below a list, the bundle of a product) must be the ones in
//     the data, in order.
//   - A page the model says is changed and whose data is the same is a dependency the model has and the
//     data does not. They are counted, by kind of write, and more than a tenth of the pairs of the
//     storefront fails, to have the model made tighter. This share is of the writes picked here, one
//     pair for each page and write: it is not weighted by the traffic and is no error of the hit rate.
//     The tenth is of the pages the file has and the lists it had (those of the landings and of the
//     crawlers). The other lists are counted apart, by class (ListClass), and printed: every list
//     depends on the price of every product, since it shows the range of all the prices, so a price
//     edit changes the version of a list of a search that shows nothing of that product.
//   - A write that sets the value a product has, or puts back an earlier one, changes the version and
//     not the data, or brings back data seen before. The model counts writes and does not look at the
//     values, so the expected rate is low for them. They are counted apart and do not fail.
//
// The pages of the back office follow a rule that changes all of both shops at once (versions.go), so
// the pairs it changes in vain are printed with their share and do not fail: how low that makes their
// expected rate is not measured.
func TestTheDataVersionsFollowTheShop(t *testing.T) {
	env := depsEnv(t)
	d, err := CheckStoreDeps(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	buildID, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Specs(), func(s Spec) bool { return s.Name == "tifa-nocache" })
	spec := env.Specs()[i]
	server, err := spec.Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	lists, listCounts := versionLists()
	t.Logf("the lists asked for: %s", strings.Join(listCounts, ", "))
	// The lists the tenth is not of: all but those of the landings and of the crawlers.
	extra := map[string]bool{}
	{
		public, _ := CrawlURLs()
		for _, u := range lists {
			extra[u] = true
		}
		for _, u := range slices.Concat(StoreVisitPages(), public) {
			delete(extra, stripLanding(u))
		}
	}
	pages := slices.Concat(depsURLs(), lists, staffPages)
	digits := func(u string) string {
		if strings.HasPrefix(u, "/projects/") {
			return ".111"
		}
		return ".11"
	}
	snapshot := func() map[string][]byte {
		out := map[string][]byte{}
		for _, u := range pages {
			sep := "?"
			if strings.Contains(u, "?") {
				sep = "&"
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", server.Base+u+sep+"_tifa="+buildID+digits(u), nil)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 {
				t.Fatalf("GET %s: %d %v %.200s", u, res.StatusCode, err, body)
			}
			out[u] = body
		}
		return out
	}
	send := func(c *http.Client, method, path string, body []byte, want int) []byte {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, server.Base+path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d: %.200s", method, path, res.StatusCode, want, data)
		}
		return data
	}
	staff := http.DefaultClient
	const seed = 3
	rec := func(slug string) storeRecord {
		i := slices.IndexFunc(storeCatalogue().records, func(r storeRecord) bool { return r.Slug == slug })
		if i < 0 {
			t.Fatalf("no product %s in the catalogue", slug)
		}
		return storeCatalogue().records[i]
	}

	var steps []versionStep
	var visits []VisitWrites // the visits that place orders, which the restock is made for
	// edit is an edit of staff that sets the price (in fen) or the stock of a product.
	edit := func(name, slug string, price, stock *int, same bool, changes ...string) {
		change := map[string]any{}
		if price != nil {
			change["priceCents"] = *price
		}
		if stock != nil {
			change["stock"] = *stock
		}
		kind := "an edit of a price"
		if price == nil {
			kind = "an edit of a stock"
		}
		steps = append(steps, versionStep{name: name, kind: kind, effects: []effect{editEffect(slug, price, stock)}, same: same, changes: changes, do: func() {
			send(staff, "PATCH", "/projects/studio/products/"+slug, mustJSON(change), 200)
		}})
	}
	// buy is a visit of a new visitor that adds the products to its cart and checks out. Its cart starts
	// with the demonstration cart, as takesOf has it.
	buys := 0
	buy := func(name string, slugs []string, changes ...string) {
		buys++
		rng := rand.New(rand.NewPCG(1, uint64(buys)))
		v := VisitWrites{}
		var adds []StoreRequest
		for _, slug := range slugs {
			adds = append(adds, storeAdd(rng, "/store", pageProduct("/store/products/"+slug)))
		}
		for j := range adds {
			v.Writes = append(v.Writes, PlannedWrite{After: 0, Seq: 100 + buys, Kind: WriteAdd, Store: &adds[j]})
		}
		checkout := StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/store/checkout", Body: []byte(storeOrder("en-US")), Status: 303}
		v.Writes = append(v.Writes, PlannedWrite{After: 2, Seq: 100 + buys, Kind: WriteCheckout, Store: &checkout})
		visits = append(visits, v)
		steps = append(steps, versionStep{name: name, kind: "a checkout", effects: versionEffects(seed, v), changes: changes, do: func() {
			jar, _ := cookiejar.New(nil)
			visitor := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			for _, a := range adds {
				send(visitor, a.Method, a.Path, a.Body, 200)
			}
			send(visitor, checkout.Method, checkout.Path, checkout.Body, 303)
		}})
	}
	// order is an order staff place, and then its ship.
	order := func(seq int) {
		create := PlannedWrite{After: 0, Seq: seq, Kind: WriteCreate, Project: storefrontShop}
		v := VisitWrites{Writes: []PlannedWrite{create}}
		visits = append(visits, v)
		var created string
		steps = append(steps, versionStep{name: fmt.Sprintf("order %d placed by staff", seq), kind: "an order placed by staff", effects: versionEffects(seed, v), do: func() {
			var placed struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(send(staff, "POST", "/projects/studio/orders", mustJSON(AdminBody(seed, seq, 0, WriteCreate, "studio")), 200), &placed); err != nil {
				t.Fatal(err)
			}
			created = placed.ID
		}})
		// The order of a ship took its pieces when it was placed (in a run, before the clock), so the ship
		// changes the back office alone.
		ship := VisitWrites{Writes: []PlannedWrite{{After: 0, Seq: seq, Kind: WriteShip, Project: "studio"}}}
		steps = append(steps, versionStep{name: fmt.Sprintf("the ship of order %d", seq), kind: "a ship", effects: versionEffects(seed, ship), do: func() {
			send(staff, "POST", "/projects/studio/orders/"+created, mustJSON(AdminBody(seed, seq, 0, WriteShip, "studio")), 200)
		}})
	}

	// The products the sequences are about, picked from the catalogue by what they are for.
	prices := byPrice()
	dearest, cheapest := rec(prices[0]), rec(prices[len(prices)-1])
	var offSale, lastPick, nextPick, inRange, soldOut storeRecord
	start := newVersionModel(d, nil)
	for _, r := range storeCatalogue().records {
		if r.CompareFen > 0 && r.Stock > 0 && !slices.Contains(start.sold(), r.Slug) && offSale.Slug == "" {
			offSale = r
		}
		if r.Stock == 0 && !r.Sized && r.CompareFen == 0 && soldOut.Slug == "" {
			soldOut = r
		}
		if p := r.PriceFen / 100; p >= 150 && p <= 250 && r.Stock > 0 && inRange.Slug == "" && r.Slug != offSale.Slug {
			inRange = r
		}
	}
	lastPick = rec(start.picked()[5])
	for _, r := range storeCatalogue().records {
		if r.Rating == lastPick.Rating && !slices.Contains(start.picked(), r.Slug) && r.Stock > 0 {
			nextPick = r
			break
		}
	}
	// A product that is in the bundle of a product page, and is in no cart here.
	bundled := rec(start.shown("/store/products/oak-tray")["bundle"][0])
	for _, r := range []storeRecord{offSale, lastPick, nextPick, inRange, soldOut} {
		if r.Slug == "" {
			t.Fatalf("the catalogue has no product for one of the sequences: off the sale %q, last pick %q, next pick %q, in the range %q, sold out %q", offSale.Slug, lastPick.Slug, nextPick.Slug, inRange.Slug, soldOut.Slug)
		}
	}
	top := EditPriceFen[1] // the highest price an edit of the mix sets
	if top <= dearest.PriceFen {
		t.Fatalf("the dearest product costs %d fen, no less than the highest price an edit sets", dearest.PriceFen)
	}
	home := []string{"/store", "/zh-CN/store"}
	byPriceList := []string{"/store/products?sort=price-desc", "/zh-CN/store/products?sort=price-desc"}

	steps = append(steps, versionStep{name: "nothing", kind: "nothing", do: func() {}})
	{
		// An add puts a product in the cart of the visitor, which no shared page shows.
		add := storeAdd(rand.New(rand.NewPCG(1, 2)), "/store", pageProduct("/store/products/walnut-pen"))
		steps = append(steps, versionStep{name: "an add to the cart", kind: "an add to the cart", effects: versionEffects(seed, VisitWrites{Writes: []PlannedWrite{{After: 0, Seq: 5, Kind: WriteAdd, Store: &add}}}), do: func() {
			jar, _ := cookiejar.New(nil)
			send(&http.Client{Jar: jar}, add.Method, add.Path, add.Body, 200)
		}})
	}
	buy("a checkout of walnut-pen", []string{"walnut-pen"}, "/store/products/walnut-pen")
	buy("a checkout of linen-apron and oak-tray", []string{"linen-apron", "oak-tray"}, "/store/products/oak-tray")

	// A product with a sale price that is not on the home comes onto its sale when it is made the dearest,
	// and onto the first page of the list by price. From then on its stock is on those pages: an edit of
	// it and an order change them. Its price put back takes it off again.
	edit("the price of "+offSale.Slug+" (off the sale) to the dearest", offSale.Slug, intp(top), nil, false, slices.Concat(home, byPriceList)...)
	edit("the stock of "+offSale.Slug+", now on the sale", offSale.Slug, nil, intp(offSale.Stock+400), false, slices.Concat(home, byPriceList)...)
	buy("a checkout of "+offSale.Slug+", now on the sale", []string{offSale.Slug}, slices.Concat(home, byPriceList)...)
	edit("the price of "+offSale.Slug+" put back", offSale.Slug, intp(offSale.PriceFen), nil, false, slices.Concat(home, byPriceList)...)
	edit("the price of "+offSale.Slug+" set to what it is", offSale.Slug, intp(offSale.PriceFen), nil, true)
	edit("the stock of "+offSale.Slug+", off the sale again", offSale.Slug, nil, intp(offSale.Stock+450), false)

	// The same for a product with no sale price: the cheapest, made the second dearest, is on the first
	// page of the list by price, and off the last.
	edit("the price of "+cheapest.Slug+" (the cheapest) to the top of the list by price", cheapest.Slug, intp(top-100), nil, false, byPriceList...)
	edit("the stock of "+cheapest.Slug+", now on the first page by price", cheapest.Slug, nil, intp(cheapest.Stock+300), false, byPriceList...)
	edit("the stock of "+cheapest.Slug+" set to what it is", cheapest.Slug, nil, intp(cheapest.Stock+300), true)
	buy("a checkout of "+cheapest.Slug+", now on the first page by price", []string{cheapest.Slug}, byPriceList...)
	edit("the price of "+cheapest.Slug+" put back", cheapest.Slug, intp(cheapest.PriceFen), nil, false, byPriceList...)

	// The best rated below every list are the cheaper first among equals: a price brings a product among
	// them, and then its stock is on every list.
	edit("the price of "+nextPick.Slug+" under the best rated", nextPick.Slug, intp(EditPriceFen[0]), nil, false, "/store/products", "/zh-CN/store/products?sort=newest")
	edit("the stock of "+nextPick.Slug+", now among the best rated", nextPick.Slug, nil, intp(nextPick.Stock+200), false, "/store/products", "/zh-CN/store/products?sort=newest")
	edit("the price of "+nextPick.Slug+" put back", nextPick.Slug, intp(nextPick.PriceFen), nil, false, "/store/products")

	// A price that leaves the range of a list takes the product off it.
	var ranged []string
	for _, u := range pages {
		if lang, kind := StorePageKind(u); kind == StoreKindList && lang == "zh-CN" {
			if f := listFilterOf(u); f.Min == 100 && f.Max == 300 {
				ranged = append(ranged, u)
			}
		}
	}
	if len(ranged) == 0 {
		t.Fatal("no Chinese list of 100 to 300 yuan among the pages")
	}
	edit("the price of "+inRange.Slug+" out of the range of 100 to 300 yuan", inRange.Slug, intp(30_100), nil, false, ranged...)
	edit("the stock of "+inRange.Slug+", out of the range", inRange.Slug, nil, intp(inRange.Stock+100), false)
	edit("the price of "+inRange.Slug+" put back", inRange.Slug, intp(inRange.PriceFen), nil, false, ranged...)

	// A product that is sold out and gets a stock, and one that is in a bundle and sells out.
	edit("a stock for "+soldOut.Slug+", which was sold out", soldOut.Slug, nil, intp(40), false, "/store/products/"+soldOut.Slug)
	edit("no stock for "+bundled.Slug+", which is in bundles", bundled.Slug, nil, intp(0), false, "/store/products/oak-tray")
	edit("a stock for "+bundled.Slug+" again", bundled.Slug, nil, intp(bundled.Stock+60), false, "/store/products/oak-tray")

	// The edits as the plans draw them: the first few of a price and of a stock.
	target := func(slug string) int { return rec(slug).Stock }
	drawn := map[string]int{}
	for seq := 0; drawn["priceCents"] < 5 || drawn["stock"] < 5; seq++ {
		w := PlannedWrite{After: 0, Seq: seq, Kind: WriteEdit, Project: storefrontShop}
		slug, change := productChange(seed, w, target)
		field := "priceCents"
		if change["stock"] != nil {
			field = "stock"
		}
		// A drawn edit of a product the sequences above sold out or left with another stock would say less.
		if drawn[field] >= 5 || slug == bundled.Slug {
			continue
		}
		drawn[field]++
		kind := "an edit of a price"
		if field == "stock" {
			kind = "an edit of a stock"
		}
		steps = append(steps, versionStep{name: fmt.Sprintf("the planned edit %d: %s of %s to %v", seq, field, slug, change[field]), kind: kind, effects: versionEffects(seed, VisitWrites{Writes: []PlannedWrite{w}}), do: func() {
			send(staff, "PATCH", "/projects/studio/products/"+slug, mustJSON(change), 200)
		}})
	}
	buy("a checkout of glass-carafe and ceramic-cup", []string{"glass-carafe", "ceramic-cup"})
	order(9)
	order(10)
	order(11)

	// The restock before the clock, as a run makes it: every product the orders take gets twice what they
	// can take, so none sells out, and the model starts from that stock.
	need := StockFor(seed, visits)
	if err := Restock(ctx, server.Base, spec, buildID, need); err != nil {
		t.Fatal(err)
	}
	m := newVersionModel(d, need[storefrontShop])

	// shownIn are the products in a part of the data of a page.
	shownIn := func(u string, body []byte, part string) []string {
		var data struct {
			Routes map[string]struct {
				Data map[string]json.RawMessage `json:"data"`
			} `json:"routes"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		var cards []struct {
			Slug string `json:"slug"`
		}
		raw, ok := data.Routes[routeOf(u)].Data[part]
		if !ok {
			t.Fatalf("%s: the data has no %s", u, part)
		}
		if err := json.Unmarshal(raw, &cards); err != nil {
			t.Fatalf("%s: %s: %v", u, part, err)
		}
		out := []string{}
		for _, c := range cards {
			out = append(out, c.Slug)
		}
		return out
	}
	wrongParts := 0
	checkParts := func(step, u string, body []byte) {
		for part, want := range m.shown(u) {
			if got := shownIn(u, body, part); !slices.Equal(got, want) {
				if wrongParts++; wrongParts <= 20 {
					t.Errorf("%s: %s of %s shows %v, and the model works out %v", step, part, u, got, want)
				}
			}
		}
	}
	stampOf := func(u string) int {
		at, err := m.stampOf(u)
		if err != nil {
			t.Fatal(err)
		}
		return at
	}

	before := snapshot()
	stamps := map[string]int{}
	versions := map[string]map[int][32]byte{} // the data a page had at each version it was asked for with
	earlier := map[string]map[[32]byte]bool{} // every data a page had
	for _, u := range pages {
		stamps[u] = stampOf(u)
		sum := sha256.Sum256(before[u])
		versions[u] = map[int][32]byte{stamps[u]: sum}
		earlier[u] = map[[32]byte]bool{sum: true}
		checkParts("after the restock", u, before[u])
	}
	for _, s := range steps {
		for _, u := range s.changes {
			if _, ok := before[u]; !ok {
				t.Fatalf("%s: %s is not a page of the check", s.name, u)
			}
		}
	}
	shop, office, sameValue := map[string]*versionTally{}, versionTally{}, versionTally{}
	more := map[string]*versionTally{ListSearch: {}, ListFilter: {}, ListOther: {}} // the lists the tenth is not of, by class
	back, missed := 0, 0
	var kinds []string
	for _, s := range steps {
		s.do()
		for _, e := range s.effects {
			m.apply(e)
		}
		after := snapshot()
		var c versionTally
		var vain []string
		for _, u := range pages {
			at := stampOf(u)
			sum := sha256.Sum256(after[u])
			model, data := at != stamps[u], string(before[u]) != string(after[u])
			// A page that has a version it had before must have the data it had then: with the version of
			// the last step when the model says the write did not change it, or of an earlier one.
			had, old := versions[u][at]
			lost := old && had != sum
			versions[u][at], stamps[u] = sum, at
			tally := &c
			switch {
			case strings.HasPrefix(u, "/projects/"):
				tally = &office
			case extra[u] && !s.same:
				tally = more[ListClass(u)]
			case extra[u]:
				tally = &versionTally{} // a write of the value a product has, on a list the tenth is not of
			}
			switch {
			case lost:
				tally.missed++
				if missed++; missed <= 20 {
					t.Errorf("%s: the data of %s changed and the model gives it a version it had before (the write changed it: %v)", s.name, u, model)
				}
			case model:
				tally.changed++
				if !data {
					tally.vain++
					if tally == &c {
						vain = append(vain, u)
					}
				} else if earlier[u][sum] && tally == &c {
					back++
				}
			}
			earlier[u][sum] = true
			if data || model {
				checkParts(s.name, u, after[u])
			}
		}
		for _, u := range s.changes {
			if string(before[u]) == string(after[u]) {
				t.Errorf("%s: the data of %s is the same, and this write is here to change it", s.name, u)
			}
		}
		into := &sameValue
		if !s.same {
			if shop[s.kind] == nil {
				shop[s.kind] = &versionTally{}
				kinds = append(kinds, s.kind)
			}
			into = shop[s.kind]
		}
		into.changed, into.vain, into.missed = into.changed+c.changed, into.vain+c.vain, into.missed+c.missed
		line := fmt.Sprintf("%s: the model changes %d pages of the storefront, %d of them with the same data, and misses %d", s.name, c.changed, c.vain, c.missed)
		if len(vain) > 0 {
			line += ", for example " + strings.Join(vain[:min(len(vain), 3)], ", ")
		}
		t.Log(line)
		before = after
	}
	var all versionTally
	for _, k := range kinds {
		c := shop[k]
		all.changed, all.vain, all.missed = all.changed+c.changed, all.vain+c.vain, all.missed+c.missed
		t.Logf("the storefront, %s: of the %d (page, write) pairs the model says changed, the data of %d did not (%.1f%%), and the model missed %d", k, c.changed, c.vain, c.share(), c.missed)
	}
	t.Logf("the storefront, all the writes picked here but those of the same value: of the %d (page, write) pairs the model says changed, the data of %d did not (%.1f%%), and the model missed %d. This is a share of pairs for these writes, not weighted by the traffic, and no error of the hit rate", all.changed, all.vain, all.share(), all.missed)
	moreMissed := 0
	for _, class := range []string{ListSearch, ListFilter, ListOther} {
		n := 0
		for u := range extra {
			if ListClass(u) == class {
				n++
			}
		}
		c := more[class]
		moreMissed += c.missed
		t.Logf("the other lists, class %s (%d lists): of the %d (page, write) pairs the model says changed, the data of %d did not (%.1f%%), and the model missed %d", class, n, c.changed, c.vain, c.share(), c.missed)
	}
	t.Logf("the storefront, writes of the value a product has: the model changed %d pairs and the data of %d was the same. Writes that put a value back: %d pairs had data the page had before under an older version. The expected rate is low for both", sameValue.changed, sameValue.vain, back)
	t.Logf("the back office (a rough rule): of the %d (page, write) pairs the model says changed, the data of %d did not (%.1f%%), and the model missed %d. How low this makes its expected rate is not measured", office.changed, office.vain, office.share(), office.missed)
	if wrongParts > 0 {
		t.Errorf("%d parts of pages show other products than the model works out", wrongParts)
	}
	if all.changed == 0 {
		t.Fatal("the model changed no page of the storefront: the writes do nothing")
	}
	if all.missed+office.missed+sameValue.missed+moreMissed+missed > 0 {
		t.Errorf("the model missed %d changes of the storefront, %d of the other lists and %d of the back office (%d in all, with the writes of the same value)", all.missed+sameValue.missed, moreMissed, office.missed, missed)
	}
	if float64(all.vain) > 0.1*float64(all.changed) {
		t.Errorf("of the %d (page, write) pairs the model says changed, the data of %d did not (%.1f%%), more than a tenth: tighten the model", all.changed, all.vain, all.share())
	}
}

// depsEnv is the environment of the tests that start the shop, which are off unless TIFA_COMPARE=1, with
// the shop built (TIFA_COMPARE_SKIP_BUILD=1 uses the last build).
func depsEnv(t *testing.T) Env {
	t.Helper()
	if os.Getenv("TIFA_COMPARE") != "1" {
		t.Skip("starts the shop, set TIFA_COMPARE=1 to run it")
	}
	if why := MissingTools(); why != "" {
		t.Skip(why)
	}
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") != "1" {
		var log strings.Builder
		if err := env.BuildShop(context.Background(), &log); err != nil {
			t.Fatalf("%v\n%s", err, &log)
		}
	}
	return env
}
