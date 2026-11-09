package compare

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
)

// The traffic mix of the one site: who visits, how many pages a visit sees, where it lands and what
// it writes. Every number has its source next to it, or says that it is an assumption. A number with
// no published data is an assumption, and the report lists each with its value.

// SiteShop is the one site the comparison plays: the storefront and the back office of the shop on
// the same server, shoppers and staff at once.
const SiteShop = "shop"

// The roles of a visit.
const (
	RoleShopper = "shopper"
	RoleStaff   = "staff"
)

const (
	// ShopperViewsMean is the page views of a shopper visit on average: the median of 438 Shopify
	// stores, 2026-08-03 to 2026-10-31 (Littledata benchmarks, https://www.littledata.io/benchmarks).
	ShopperViewsMean = 2.6
	// ShopperViewsMax is the most page views of a shopper visit, an assumption: the top 10% of the
	// stores of the same source have 5.0 pages a session.
	ShopperViewsMax = 5
	// AddToCartShare is the share of shopper visits that add to the cart: the median of the same
	// source.
	AddToCartShare = 0.066
	// CheckoutShare is the share of shopper visits that place an order: the median conversion rate of
	// the same source. A visit that checks out has added to the cart first, so these are among the
	// visits of AddToCartShare.
	CheckoutShare = 0.013
	// ReturningShare is the share of shopper visits by a returning visitor, who comes back to a cart
	// of their own. An assumption within the published range: 17% (33 DTC stores, Top Growth
	// Marketing, https://topgrowthmarketing.com/dtc-ecommerce-benchmarks/returning-vs-new-customers/)
	// to 53% (6000 sites, Contentsquare 2025,
	// https://contentsquare.com/en/guides/digital-experience-benchmark/conversions/).
	ReturningShare = 0.20
	// ProductZipf is the exponent of the popularity of the products: the product of rank k is seen
	// about 1/k^s as often as the first, ranked in the order of the catalogue. An inference: sales follow
	// a power law (Brynjolfsson, Hu and Simester, Management Science 2011), and a rough fit of ebook
	// sales by rank gives an exponent of about 0.93. No data on product views was found.
	ProductZipf = 1.0
	// StaffShare is the share of the visits of the site made by the staff in the back office. An
	// assumption: no data was found.
	StaffShare = 0.02
	// StaffViewsMean and StaffViewsMax are the page views of a staff visit on average and at most. An
	// assumption.
	StaffViewsMean = 4
	StaffViewsMax  = 8
	// StaffEditShare is the share of staff visits that edit a product (its price or its stock), and
	// StaffShipShare the share that ship an order. Assumptions. A staff visit does one or the other or
	// neither.
	StaffEditShare = 0.20
	StaffShipShare = 0.30
	// EditPriceShare is the share of the edits of a product that change its price. The others change
	// its stock. An assumption.
	EditPriceShare = 0.5
)

// EditPriceFen is the lowest and the highest price an edit of a price sets, in fen, in whole yuan, and
// EditDelivery the fewest and the most pieces an edit of the stock sets above what the restock gave
// the product. Assumptions.
var (
	EditPriceFen = [2]int{1_900, 99_900}
	EditDelivery = [2]int{5, 50}
	// AddPieces is the fewest and the most pieces an add puts in the cart. An assumption.
	AddPieces = [2]int{1, 3}
)

// LandingShares are the shares of the kinds of page a shopper visit lands on. An assumption: no
// reliable data was found (one unchecked lead says about a quarter of visitors land on a product).
// The two languages of the storefront get half of the visits each, an assumption too.
var LandingShares = []struct {
	Kind  string
	Share float64
}{{StoreKindHome, 0.30}, {StoreKindList, 0.25}, {StoreKindProduct, 0.45}}

// A block of the plans holds ShopperBlockVisits shopper visits, which have the mix above exactly
// (2600 page views, 66 adds to the cart of which 13 check out, 200 returning visitors, the landing
// pages and languages by quota), and StaffBlockPlans staff visits. StaffBlockPlans is not a share:
// it is how many different staff visits are recorded. How many staff visits a cycle of the site plays
// is the scenario's (BrowseScenario.Staff).
const (
	ShopperBlockVisits = 1000
	StaffBlockPlans    = 50
	SiteBlockPlans     = ShopperBlockVisits + StaffBlockPlans
)

// productWeight is the weight of a product by its rank in the catalogue (StoreProducts), by the Zipf
// law of ProductZipf.
func productWeight(slug string) float64 {
	i := slices.Index(StoreProducts, slug)
	if i < 0 {
		panic("no product " + slug)
	}
	return math.Pow(float64(i+1), -ProductZipf)
}

// productOf is the product a storefront URL of a product page shows.
func productOf(url string) string {
	path, _, _ := strings.Cut(url, "?")
	return path[strings.LastIndex(path, "/")+1:]
}

// visitLengths draws n visit lengths of 1 to most page views that add up to exactly total: a
// geometric draw that goes on with probability more, then moved by one view at a time until they add
// up.
func visitLengths(rng *rand.Rand, n, most, total int, more float64) []int {
	lens := make([]int, n)
	sum := 0
	for i := range lens {
		k := 1
		for k < most && rng.Float64() < more {
			k++
		}
		lens[i] = k
		sum += k
	}
	for sum != total {
		i := rng.IntN(n)
		switch {
		case sum > total && lens[i] > 1:
			lens[i]--
			sum--
		case sum < total && lens[i] < most:
			lens[i]++
			sum++
		}
	}
	return lens
}

// GenerateSitePlanSet makes blocks blocks of plans of the site. A block depends on the seed and its
// number only, and holds ShopperBlockVisits shopper plans and then StaffBlockPlans staff plans.
//
// A shopper visit stays in one language (half of the visits each), lands on a page by LandingShares
// (a list in any of its filtered forms alike, a product by the Zipf law of ProductZipf) and clicks the
// links of the page on screen by storeClicks, the products among them by the Zipf law too. Its length
// is 1 to ShopperViewsMax, ShopperViewsMean on average exactly. All of these are by quota in each
// block. Then, by a seeded shuffle, CheckoutShare of the visits that see a product and later the
// checkout add to the cart after that product and check out after the checkout page, and the rest of
// AddToCartShare among the visits that see a product add to the cart after the first product they see.
// ReturningShare of the visits are returning visitors.
//
// A staff visit is 1 to StaffViewsMax page views of the back office, StaffViewsMean on average
// exactly, landing on a list or a stats page for half of them and on an order for the other half, and
// clicking the links every page of the back office has (Links).
func GenerateSitePlanSet(seed uint64, blocks int) PlanSet {
	set := PlanSet{Seed: seed, Site: SiteShop, Blocks: blocks}
	byKind := map[string][]string{}
	for _, url := range StoreVisitPages() {
		l, k := StorePageKind(url)
		byKind[l+" "+k] = append(byKind[l+" "+k], url)
	}
	var kinds []string
	var shares []float64
	for _, s := range LandingShares {
		kinds, shares = append(kinds, s.Kind), append(shares, s.Share)
	}
	share := func(s float64) int { return int(math.Round(s * ShopperBlockVisits)) }
	var hubs, orders []string
	for _, p := range AllPages() {
		if k, _, _ := PageKind(p.URL); k == "order" {
			orders = append(orders, p.URL)
		} else {
			hubs = append(hubs, p.URL)
		}
	}
	for b := range blocks {
		rng := rand.New(rand.NewPCG(seed^0x6a09e667f3bcc909, uint64(b)))
		lens := visitLengths(rng, ShopperBlockVisits, ShopperViewsMax, share(ShopperViewsMean), 0.665)
		langs := slices.Clone(StoreLangs)
		if b%2 == 1 {
			slices.Reverse(langs)
		}
		ls := quota(rng, ShopperBlockVisits, langs, []float64{0.5, 0.5})
		ks := quota(rng, ShopperBlockVisits, kinds, shares)
		var shoppers []Plan
		for i := range ShopperBlockVisits {
			var first string
			switch pages := byKind[ls[i]+" "+ks[i]]; ks[i] {
			case StoreKindProduct:
				var links []Link
				for _, url := range pages {
					links = append(links, Link{url, productWeight(productOf(url))})
				}
				var sum float64
				for _, l := range links {
					sum += l.Weight
				}
				for j := range links {
					links[j].Weight /= sum
				}
				first = pick(rng, links)
			default:
				first = pages[rng.IntN(len(pages))]
			}
			steps := []PlanStep{{StepLoad, first}}
			for len(steps) < lens[i] {
				steps = append(steps, PlanStep{StepClick, pick(rng, StoreLinks(steps[len(steps)-1].Page))})
			}
			shoppers = append(shoppers, Plan{ID: b*SiteBlockPlans + i, Block: b, Role: RoleShopper, Steps: steps})
		}
		// The writes: the visits that can check out (a product, then the checkout) and the ones that can
		// add to the cart (a product), in a seeded shuffle.
		order := rng.Perm(ShopperBlockVisits)
		kindAt := func(p Plan, kind string, from int) int {
			for j := from; j < len(p.Steps); j++ {
				if _, k := StorePageKind(p.Steps[j].Page); k == kind {
					return j
				}
			}
			return -1
		}
		checkouts, adds := share(CheckoutShare), share(AddToCartShare)
		// A visit that checks out walks again, from the same page and for as many page views, until it
		// sees a product and later the checkout: it can when it lands on a product and has two page
		// views or more (the product, then the checkout from the cart drawer), or has three or more.
		for _, i := range order {
			p := &shoppers[i]
			if checkouts == 0 {
				break
			}
			if _, k := StorePageKind(p.Steps[0].Page); len(p.Steps) < 2 || len(p.Steps) == 2 && k != StoreKindProduct {
				continue
			}
			walk := rand.New(rand.NewPCG(seed^0xbb67ae8584caa73b, uint64(b)<<20|uint64(i)))
			for try := 0; ; try++ {
				if at := kindAt(*p, StoreKindProduct, 0); at >= 0 && kindAt(*p, StoreKindCheckout, at+1) >= 0 {
					break
				}
				if try == 100_000 {
					panic(fmt.Sprintf("block %d of seed %d: visit %d found no walk to the checkout", b, seed, i))
				}
				for j := 1; j < len(p.Steps); j++ {
					p.Steps[j].Page = pick(walk, StoreLinks(p.Steps[j-1].Page))
				}
			}
			if at := kindAt(*p, StoreKindProduct, 0); at >= 0 {
				if out := kindAt(*p, StoreKindCheckout, at+1); out >= 0 {
					// The last product seen before the checkout.
					for j := at + 1; j < out; j++ {
						if _, k := StorePageKind(p.Steps[j].Page); k == StoreKindProduct {
							at = j
						}
					}
					p.Writes = []PlanWrite{{at, WriteAdd}, {out, WriteCheckout}}
					checkouts--
					adds--
				}
			}
		}
		for _, i := range order {
			p := &shoppers[i]
			if adds == 0 {
				break
			}
			if at := kindAt(*p, StoreKindProduct, 0); at >= 0 && len(p.Writes) == 0 {
				p.Writes = []PlanWrite{{at, WriteAdd}}
				adds--
			}
		}
		if checkouts > 0 || adds > 0 {
			panic(fmt.Sprintf("block %d of seed %d: %d checkouts and %d adds found no visit that can make them", b, seed, checkouts, adds))
		}
		for _, i := range rng.Perm(ShopperBlockVisits)[:share(ReturningShare)] {
			shoppers[i].Returning = true
		}
		set.Plans = append(set.Plans, shoppers...)
		// The staff: half of the visits land on a list or a stats page, half on an order.
		slens := visitLengths(rng, StaffBlockPlans, StaffViewsMax, StaffViewsMean*StaffBlockPlans, 0.75)
		firsts := quota(rng, StaffBlockPlans, []string{"hub", "order"}, []float64{0.5, 0.5})
		for i := range StaffBlockPlans {
			pages := hubs
			if firsts[i] == "order" {
				pages = orders
			}
			steps := []PlanStep{{StepLoad, pages[rng.IntN(len(pages))]}}
			for len(steps) < slens[i] {
				steps = append(steps, PlanStep{StepClick, pick(rng, Links(steps[len(steps)-1].Page))})
			}
			set.Plans = append(set.Plans, Plan{ID: b*SiteBlockPlans + ShopperBlockVisits + i, Block: b, Role: RoleStaff, Steps: steps})
		}
	}
	return set
}

// PlanCounts says what a set of plans, or the visits of a round, hold.
type PlanCounts struct {
	Visits    int `json:"visits"`
	Shoppers  int `json:"shoppers"`
	Staff     int `json:"staff"`
	Views     int `json:"pageViews"`
	Loads     int `json:"fullLoads"`
	Clicks    int `json:"clicks"`
	Returning int `json:"returning"`
	Adds      int `json:"adds"`
	Checkouts int `json:"checkouts"`
	// ShopperViews are the page views of the shopper visits.
	ShopperViews int `json:"shopperPageViews"`
	// Pages counts the page views by class (pageClass).
	Pages map[string]int `json:"pagesByClass"`
}

// Add counts a plan.
func (c *PlanCounts) Add(p Plan) {
	c.Visits++
	c.Views += len(p.Steps)
	if c.Pages == nil {
		c.Pages = map[string]int{}
	}
	switch p.Role {
	case RoleShopper:
		c.Shoppers++
		c.ShopperViews += len(p.Steps)
		if p.Returning {
			c.Returning++
		}
		for _, w := range p.Writes {
			switch w.Kind {
			case WriteAdd:
				c.Adds++
			case WriteCheckout:
				c.Checkouts++
			}
		}
	case RoleStaff:
		c.Staff++
	}
	for _, s := range p.Steps {
		if s.Kind == StepLoad {
			c.Loads++
		} else {
			c.Clicks++
		}
		c.Pages[pageClass(s.Page)]++
	}
}

// MixFact is one number of the mix as the report lists it: what it is, its value, and where it comes
// from ("assumption" when nothing published backs it).
type MixFact struct {
	What, Value, Source string
}

// MixFacts are the numbers of the mix of mix.go, with their sources, for the report.
func MixFacts() []MixFact {
	pct := func(v float64) string { return strconv.FormatFloat(math.Round(v*1e4)/100, 'f', -1, 64) + "%" }
	littledata := "median of 438 Shopify stores, 2026-08-03 to 2026-10-31 (Littledata, https://www.littledata.io/benchmarks)"
	var landing []string
	for _, l := range LandingShares {
		landing = append(landing, fmt.Sprintf("%s %s", l.Kind, pct(l.Share)))
	}
	return []MixFact{
		{"page views of a shopper visit", fmt.Sprintf("%.1f on average", ShopperViewsMean), littledata},
		{"most page views of a shopper visit", strconv.Itoa(ShopperViewsMax), "assumption (the top 10% of the same stores have 5.0)"},
		{"shopper visits that add to the cart", pct(AddToCartShare), littledata},
		{"shopper visits that check out (among those that add)", pct(CheckoutShare), littledata + ", conversion rate"},
		{"shopper visits by a returning visitor, who comes back to a cart of their own", pct(ReturningShare), "assumption, within the published 17% (Top Growth Marketing) to 53% (Contentsquare 2025)"},
		{"first page of a shopper visit", strings.Join(landing, ", "), "assumption"},
		{"language of a shopper visit", "half English, half Chinese", "assumption"},
		{"popularity of the products", fmt.Sprintf("Zipf, exponent %.1f, by rank in the catalogue", ProductZipf), "inference: sales follow a power law (Brynjolfsson, Hu and Simester, Management Science 2011)"},
		{"links a shopper clicks", "a share for each kind of link of the page (store_plan.go)", "assumption"},
		{"visits made by the staff in the back office", pct(StaffShare), "assumption"},
		{"page views of a staff visit", fmt.Sprintf("%d on average, at most %d", StaffViewsMean, StaffViewsMax), "assumption"},
		{"staff visits that edit a product", pct(StaffEditShare), "assumption"},
		{"staff visits that ship an order", pct(StaffShipShare), "assumption"},
	}
}
