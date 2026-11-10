package compare

import (
	"cmp"
	"fmt"
	"maps"
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
	// ShopperViewsMean is the page views of a shopper visit on average, exactly in every block: one
	// landing and three actions on the page, so that the landings are a quarter of the page views. The
	// landing is the first step of a plan, and an action a click on a link of the page on screen, done
	// the way the page does it: a client navigation for a link the router takes, a document load for a
	// plain link. So the quarter is of the landings, not of the documents the browser asks for, which
	// are counted from the traces (Workload.docSteps). A rule of the comparison, from about 4.3 page
	// views a session over 6,000 sites (Contentsquare 2025 Digital Experience Benchmarks: 389 billion
	// page views in 90 billion sessions,
	// https://contentsquare.com/press/2025-digital-experience-benchmarks/), with a click that a
	// single-page app takes as a client navigation a page view as a document load is, as Chrome now
	// measures each one that changes the URL as a navigation of its own
	// (https://web.dev/blog/crux-experimental-soft-navs).
	ShopperViewsMean = 4.0
	// ShopperViewsMax is the most page views of a shopper visit: the lengths are geometric of mean
	// ShopperViewsMean (ShopperViewsMore), cut at 12.
	ShopperViewsMax = 12
	// AddToCartShare is the share of shopper visits that add to the cart: the median of 438 Shopify
	// stores, 2026-08-03 to 2026-10-31 (Littledata benchmarks, https://www.littledata.io/benchmarks).
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
	// ClickIDShare is the share of the shopper landings that carry a click id that is different for every click (gclid, fbclid, msclkid), and
	// EmailParamShare the share that carry a parameter that is different for every recipient of an email
	// (_kx, mc_eid). A visit carries at most one of them. A server that keeps a page by its URL meets a URL of its own
	// on every one. Sources: of the visits to retail sites, 50.6% come from paid sources (Contentsquare
	// 2025 Digital Experience Benchmarks), which is an upper bound, and paid search is 22% of the visits
	// to a shop, and email 7.6% (US, gripsintelligence,
	// https://gripsintelligence.com/insights/web-statistics/channel-market-share/us). ClickIDShare is a
	// judgment below the two upper bounds. EmailParamShare is an assumption below the 7.6% of the visits
	// that come from email, since not every link of an email carries a parameter that is different for
	// each recipient.
	ClickIDShare    = 0.15
	EmailParamShare = 0.05
	// CrawlerShare is the share of the HTML requests that crawlers make: the documents the browsers of the
	// shoppers and of the staff ask for (the landing of each visit, and every action the page does as a
	// document load: a click the router takes fetches data, not a document) and the requests of
	// crawlers. Cloudflare reports Googlebot alone as 4.5% of the HTML requests and the other
	// AI crawlers together as 4.2% (Cloudflare Radar, 2025 year in review,
	// https://blog.cloudflare.com/radar-2025-year-in-review/), which makes about 9%. A crawler request is
	// not a visit and not a page view (crawler.go).
	CrawlerShare = 0.09
	// CrawlOldShare is the share of the requests of a crawler for an old URL that the shop no longer has
	// (it answers 404), and the rest are for the public pages, all alike. An assumption: crawlers go back
	// to old URLs and deep pages that people no longer visit, which the log file analysis of Screaming Frog
	// shows with no figure to take
	// (https://www.screamingfrog.co.uk/what-i-learnt-from-analysing-7-million-log-file-events/).
	CrawlOldShare = 0.05
	// EditPriceShare is the share of the edits of a product that change its price. The others change
	// its stock. An assumption.
	EditPriceShare = 0.5
	// SearchZipf is the exponent of the popularity of the search words of a language: the word of rank k
	// is searched for about 1/k^s as often as the first, ranked in the order the catalogue first has them
	// (store_terms.go). An assumption: the searches of a shop follow a power law with a long tail (at Best
	// Buy over 80% of the distinct queries were searched 3 times or fewer in 6 months, arXiv 2505.01946,
	// https://arxiv.org/html/2505.01946v1), and no exponent was found.
	SearchZipf = 1.0
	// SearchTermsMin and SearchTermsMax are the fewest and the most search words of a language. An
	// assumption for a shop of 150 products: no data on how many distinct words a small shop is searched
	// for was found. A list the catalogue leaves short of the fewest is filled with misspellings.
	SearchTermsMin = 300
	SearchTermsMax = 500
	// SearchNoResultShare is the share of the searches that find nothing. An assumption: 10% to 15% is
	// the figure most often given (Hello Retail, https://helloretail.com/en/blog/2026-02-24-ecommerce-search-statistics/,
	// which names no source for it), and no sample that can be checked was found. The low end, for a
	// small catalogue.
	SearchNoResultShare = 0.10
	// SearchUniqueShare is the share of the searches whose word no other visit searches for: a model
	// number, a word with digits, a misspelt sentence. Such a word finds nothing in this shop, so these
	// are among the searches of SearchNoResultShare, and the rest of those search for a word of the list
	// that finds nothing. An assumption, from one account of a shop where random tail queries with numbers
	// were 5% of the searches (an article of Ecommerce-Nation about one client, read in a summary only).
	SearchUniqueShare = 0.05
	// SearchUniqueLength is the characters of a search word of a visit's own, lower case letters and
	// digits. An assumption: no data on the shape of such words was found.
	SearchUniqueLength = 8
	// SearchShare is the share of the shopper visits that search, once, with the search box of the
	// header. An assumption below the one behavioural figure found: searchers are 24% of the visitors of
	// 113 retail sites, October to December 2024 (Constructor, "Beyond Relevance", March 2025, as
	// reported by https://itbrief.co.uk/story/online-shopper-search-drives-44-of-ecommerce-revenue),
	// taken lower for a shop of 150 products, whose menu is enough to find them. One search a visit is an
	// assumption too.
	SearchShare = 0.20
	// ListFilterShare is the share that filter the list, of the shopper visits that see a product list,
	// and ListSortShare the share of them that change its sort. Assumptions: the ranges that are
	// published for the use of filters (20% to 60%) name no source that can be checked, and nothing was
	// found on sorting.
	ListFilterShare = 0.25
	ListSortShare   = 0.10
)

// The kinds of condition a filter of the product list sets.
const (
	FilterPrice    = "price"
	FilterCategory = "category"
	FilterRating   = "rating"
	FilterStock    = "stock"
	FilterColor    = "colour"
	FilterMaterial = "material"
	FilterBadge    = "badge"
)

// FilterConditionShares are the shares of the filters that set one, two and three conditions. An
// assumption: no data on how many filters a visit uses was found.
var FilterConditionShares = []float64{0.60, 0.30, 0.10}

// FilterKinds are the kinds of condition with the relative weight each is chosen by. Assumptions: no
// data on which filters are used most was found, and the order (price and category first, then rating
// and stock, then colour, material and badge) is a judgment. A condition on the price is one of the
// four price bands of the page, never a range typed by hand.
var FilterKinds = []struct {
	Kind   string
	Weight float64
}{{FilterPrice, 25}, {FilterCategory, 25}, {FilterRating, 12.5}, {FilterStock, 12.5}, {FilterColor, 8.3}, {FilterMaterial, 8.3}, {FilterBadge, 8.3}}

// ListPageShares are the shares that stay on the first page, go to the second and go on to the third,
// of the shopper visits that see a product list of more than one page with no search term. From
// Luigi's Box, 4,000 shops: 88.7% of the customers stay on the first page of search results, 9.7% go
// to the second and 1.6% deeper (https://www.luigisbox.com/blog/product-ranking-data/, which says
// neither when nor how it was counted), rounded. The figure is of search results, and it is used on
// the lists with no search term because a search of this shop never has a second page: no word finds
// more products than a page holds.
var ListPageShares = []float64{0.90, 0.09, 0.01}

// EditPriceFen is the lowest and the highest price an edit of a price sets, in fen, in whole yuan, and
// EditDelivery the fewest and the most pieces an edit of the stock sets above what the restock gave
// the product. Assumptions.
var (
	EditPriceFen = [2]int{1_900, 99_900}
	EditDelivery = [2]int{5, 50}
	// AddPieces is the fewest and the most pieces an add puts in the cart. An assumption.
	AddPieces = [2]int{1, 3}
)

// ShopperViewsMore and StaffViewsMore are how likely a shopper and a staff visit go on to one more
// page view, in the first draw of the lengths of the visits of a block, before the lengths are moved
// one view at a time to the mean exactly (visitLengths). They shape how the lengths spread around the
// mean: 0.75 is a geometric length of mean 4. Assumptions.
const (
	ShopperViewsMore = 0.75
	StaffViewsMore   = 0.75
)

// ReturningCartLines is how many products the cart of a returning visitor holds besides the lines of
// the demonstration cart: draws of a product in stock, a colour, a size and a quantity, so that two
// returning visitors almost never hold the same cart. An assumption.
const ReturningCartLines = 3

// LanguageShares are the shares of the shopper visits in each language of the storefront. An
// assumption.
var LanguageShares = map[string]float64{"en-US": 0.5, "zh-CN": 0.5}

// ClickIDNames and EmailParamNames are the names of the parameters and the shares of the landings
// that carry a click id or an email parameter each has: gclid, fbclid and msclkid by 60 : 30 : 10, and
// _kx and mc_eid by half each. Assumptions: no data on how the names share was found.
var (
	ClickIDNames = []struct {
		Name  string
		Share float64
	}{{"gclid", 0.6}, {"fbclid", 0.3}, {"msclkid", 0.1}}
	EmailParamNames = []struct {
		Name  string
		Share float64
	}{{"_kx", 0.5}, {"mc_eid", 0.5}}
)

// CrawlerClasses are the classes of crawlers with their relative weights: the search class, which is
// Googlebot alone, 4.5 and the AI class 4.2, as Cloudflare reports them as shares of the HTML requests
// (https://blog.cloudflare.com/radar-2025-year-in-review/). Here they only weigh the classes against
// each other, since the total is fixed at CrawlerShare. Cloudflare has no figure for the other search
// engine crawlers, so none is in the mix.
var CrawlerClasses = []struct {
	Name  string
	Share float64
}{{CrawlerSearch, 4.5}, {CrawlerAI, 4.2}}

// CrawlerAgents are the crawlers of each class, with their shares among the crawlers of the class and
// the User-Agent each sends. Googlebot sends a smartphone and a desktop User-Agent, half of its
// requests each (an assumption). The AI crawlers, their shares and their User-Agents are assumptions
// too: no published share of each was found.
var CrawlerAgents = []CrawlerAgent{
	{CrawlerSearch, "Googlebot (smartphone)", 50, "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.6422.175 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
	{CrawlerSearch, "Googlebot (desktop)", 50, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/125.0.6422.175 Safari/537.36"},
	{CrawlerAI, "GPTBot", 40, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.2; +https://openai.com/gptbot"},
	{CrawlerAI, "ClaudeBot", 25, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"},
	{CrawlerAI, "Meta-ExternalAgent", 20, "meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)"},
	{CrawlerAI, "Bytespider", 10, "Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)"},
	{CrawlerAI, "PerplexityBot", 5, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)"},
}

// OldProductsPerLanguage and OldPostsPerLanguage are how many old URLs of each kind the set O of a
// crawler holds in a language (crawl.go): products taken off the shop and posts that were moved or
// deleted, which answer 404. Assumptions: no data on how many a shop that has existed for a few years
// holds.
const (
	OldProductsPerLanguage = 15
	OldPostsPerLanguage    = 15
)

// LandingValueLengths are the characters of a value of each parameter, which a request carries with
// the same length as the real one: gclid about 95, fbclid about 61, msclkid 32 hexadecimal, mc_eid 10
// hexadecimal and _kx about 40. The lengths are what the parameters are seen to have, and the
// characters of each, the shape of a value, are an assumption.
var LandingValueLengths = map[string]int{"gclid": 95, "fbclid": 61, "msclkid": 32, "_kx": 40, "mc_eid": 10}

// StoreClicks says, for the kind of page a visitor is on, the relative weight of a click on a link of
// each kind. They are assumptions, made shares among the links the page has: a visitor on a list picks a
// product, on a product looks at a related one, goes back to the list, to the cart or to the
// checkout (the link of the cart drawer), and from the cart to the checkout. A kind the page has no
// link to is left out, and the rest count for the whole. Among the links of a kind, a product is
// picked by its popularity (productWeight), any other page alike.
var StoreClicks = map[string]map[string]float64{
	StoreKindHome:     {StoreKindList: 0.3, StoreKindProduct: 0.4, StoreKindJournal: 0.15, StoreKindCart: 0.1, StoreKindPost: 0.05},
	StoreKindList:     {StoreKindProduct: 0.7, StoreKindList: 0.1, StoreKindHome: 0.1, StoreKindCart: 0.1},
	StoreKindProduct:  {StoreKindProduct: 0.3, StoreKindList: 0.25, StoreKindCart: 0.25, StoreKindCheckout: 0.1, StoreKindHome: 0.1, StoreKindJournal: 0.1},
	StoreKindJournal:  {StoreKindPost: 0.7, StoreKindHome: 0.15, StoreKindList: 0.15},
	StoreKindPost:     {StoreKindJournal: 0.4, StoreKindProduct: 0.3, StoreKindHome: 0.15, StoreKindList: 0.15},
	StoreKindCart:     {StoreKindCheckout: 0.4, StoreKindProduct: 0.3, StoreKindList: 0.2, StoreKindHome: 0.1},
	StoreKindCheckout: {StoreKindCart: 0.4, StoreKindHome: 0.3, StoreKindList: 0.3},
}

// StaffLandingShares are the shares of the staff visits that land on a list or a stats page ("hub")
// and on an order. An assumption.
var StaffLandingShares = []struct {
	Kind  string
	Share float64
}{{"hub", 0.5}, {"order", 0.5}}

// StaffClicks says, for the kind of page of the back office a member of staff is on, the relative
// weight of a click on a link of each kind (Links): from a list to its stats, to the list of the other
// shop or to one of its newest orders (alike), from an order to its list, its stats or the other
// shop, and from the stats to its list or the other shop. Assumptions.
var StaffClicks = map[string]map[string]float64{
	"list":  {"stats": 0.2, "other": 0.2, "order": 0.6},
	"order": {"list": 0.5, "stats": 0.25, "other": 0.25},
	"stats": {"list": 0.7, "other": 0.3},
}

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
	i := slices.Index(StoreProducts(), slug)
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
// links of the page on screen by StoreClicks, the products among them by the Zipf law too. Its length
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
		lens := visitLengths(rng, ShopperBlockVisits, ShopperViewsMax, share(ShopperViewsMean), ShopperViewsMore)
		langs := slices.Clone(StoreLangs)
		if b%2 == 1 {
			slices.Reverse(langs)
		}
		var lshares []float64
		for _, l := range langs {
			lshares = append(lshares, LanguageShares[l])
		}
		ls := quota(rng, ShopperBlockVisits, langs, lshares)
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
		putLandingParams(seed, b, shoppers)
		set.Plans = append(set.Plans, shoppers...)
		// The staff: half of the visits land on a list or a stats page, half on an order.
		slens := visitLengths(rng, StaffBlockPlans, StaffViewsMax, StaffViewsMean*StaffBlockPlans, StaffViewsMore)
		var skinds []string
		var sshares []float64
		for _, s := range StaffLandingShares {
			skinds, sshares = append(skinds, s.Kind), append(sshares, s.Share)
		}
		firsts := quota(rng, StaffBlockPlans, skinds, sshares)
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
	// ClickIDs and EmailParams count the shopper landings that carry a click id and an email parameter.
	ClickIDs    int `json:"clickIds"`
	EmailParams int `json:"emailParams"`
	Adds        int `json:"adds"`
	Checkouts   int `json:"checkouts"`
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
		if name := landingParam(p.Steps[0].Page); isClickID(name) {
			c.ClickIDs++
		} else if name != "" {
			c.EmailParams++
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
// MixFact is a number of the mix for the report: the name it has in mix.go, what it is, its value
// as the plans use it, and where it comes from.
type MixFact struct {
	Name, What, Value, Source string
}

// searchTermCounts writes the fewest words of a list with the lists as they are: the words of each
// language, those that find a product and those that find nothing.
func searchTermCounts(fewest int) string {
	out := strconv.Itoa(fewest)
	for _, l := range StoreLangs {
		t := StoreSearchTerms()[l]
		out += fmt.Sprintf(". %s has %d: %d find a product and %d find nothing, %d of them misspellings", l, len(t.Found)+len(t.None), len(t.Found), len(t.None), t.Misspelt)
	}
	return out
}

// sharesOf writes shares as percents, in order.
func sharesOf(shares []float64) string {
	out := make([]string, len(shares))
	for i, s := range shares {
		out[i] = strconv.FormatFloat(math.Round(s*1e4)/100, 'f', -1, 64) + "%"
	}
	return strings.Join(out, ", ")
}

// filterKinds writes the kinds of condition with their weights, in order.
func filterKinds() string {
	out := make([]string, len(FilterKinds))
	for i, k := range FilterKinds {
		out[i] = k.Kind + " " + strconv.FormatFloat(k.Weight, 'f', -1, 64)
	}
	return strings.Join(out, ", ")
}

// MixFacts are the numbers of the mix of mix.go, with their sources, for the report. Each value is
// read from the constant the plans use, and TestTheReportListsEveryNumberOfTheMix checks that every
// constant of mix.go is here.
func MixFacts() []MixFact {
	pct := func(v float64) string { return strconv.FormatFloat(math.Round(v*1e4)/100, 'f', -1, 64) + "%" }
	shares := func(m map[string]float64) string {
		var out []string
		for _, k := range slices.Sorted(maps.Keys(m)) {
			out = append(out, k+" "+pct(m[k]))
		}
		return strings.Join(out, ", ")
	}
	// clicks writes a click table as the relative weights it holds, the heaviest first: a page
	// makes them shares among the kinds of link it has, so they are not percents of anything.
	clicks := func(m map[string]map[string]float64) string {
		var out []string
		for _, k := range slices.Sorted(maps.Keys(m)) {
			kinds := slices.SortedFunc(maps.Keys(m[k]), func(a, b string) int {
				return cmp.Or(cmp.Compare(m[k][b], m[k][a]), cmp.Compare(a, b))
			})
			var ws []string
			for _, kind := range kinds {
				ws = append(ws, kind+" "+strconv.FormatFloat(math.Round(m[k][kind]*100), 'f', -1, 64))
			}
			out = append(out, "from "+k+": "+strings.Join(ws, " : "))
		}
		return strings.Join(out, "; ")
	}
	ids := func(names []struct {
		Name  string
		Share float64
	}) string {
		var out []string
		for _, n := range names {
			out = append(out, n.Name+" "+pct(n.Share))
		}
		return strings.Join(out, ", ")
	}
	lengths := func(m map[string]int) string {
		var out []string
		for _, k := range slices.Sorted(maps.Keys(m)) {
			out = append(out, k+" "+strconv.Itoa(m[k]))
		}
		return strings.Join(out, ", ")
	}
	crawlerClasses := func() string {
		var out []string
		for _, k := range CrawlerClasses {
			out = append(out, k.Name+" "+strconv.FormatFloat(k.Share, 'f', -1, 64))
		}
		return strings.Join(out, ", ")
	}
	crawlerAgents := func() string {
		var out []string
		for _, a := range CrawlerAgents {
			out = append(out, fmt.Sprintf("%s %s %g%%", a.Class, a.Name, a.Share))
		}
		return strings.Join(out, "; ")
	}
	littledata := "median of 438 Shopify stores, 2026-08-03 to 2026-10-31 (Littledata, https://www.littledata.io/benchmarks)"
	var landing, staffLanding []string
	for _, l := range LandingShares {
		landing = append(landing, fmt.Sprintf("%s %s", l.Kind, pct(l.Share)))
	}
	for _, l := range StaffLandingShares {
		staffLanding = append(staffLanding, fmt.Sprintf("%s %s", l.Kind, pct(l.Share)))
	}
	return []MixFact{
		{"ShopperViewsMean", "page views of a shopper visit", fmt.Sprintf("%g on average, exactly in every block: 1 landing and %g actions on the page (an action is a click on a link of the page, which the page does as a client navigation, or as a document load for a plain link)", ShopperViewsMean, ShopperViewsMean-1),
			"a rule of the comparison, from about 4.3 page views a session over 6,000 sites (Contentsquare 2025, https://contentsquare.com/press/2025-digital-experience-benchmarks/), a click that a single-page app takes as a client navigation counted as a page view as Chrome measures it (web.dev, https://web.dev/blog/crux-experimental-soft-navs)"},
		{"ShopperViewsMax", "most page views of a shopper visit", strconv.Itoa(ShopperViewsMax), "rule: geometric lengths of mean 4, cut at 12"},
		{"ShopperViewsMore", "chance a shopper visit goes on to one more page, in the first draw of the lengths", pct(ShopperViewsMore), "rule: a geometric length of mean 4"},
		{"AddToCartShare", "shopper visits that add to the cart", pct(AddToCartShare), littledata},
		{"CheckoutShare", "shopper visits that check out (among those that add)", pct(CheckoutShare), littledata + ", conversion rate"},
		{"ClickIDShare", "shopper landings that carry a click id different for every click", pct(ClickIDShare), "assumption: a judgment below the published upper bounds, 50.6% of the visits of retail sites from paid sources (Contentsquare 2025) and 22% from paid search (US, gripsintelligence, https://gripsintelligence.com/insights/web-statistics/channel-market-share/us)"},
		{"ClickIDNames", "click ids, by the share of the landings that carry one", ids(ClickIDNames), "assumption"},
		{"EmailParamShare", "shopper landings that carry a parameter different for every recipient of an email", pct(EmailParamShare), "assumption: below the 7.6% (US, gripsintelligence) of the visits that come from email (https://gripsintelligence.com/insights/web-statistics/channel-market-share/us), since not every link of an email carries a parameter that is different for each recipient"},
		{"EmailParamNames", "email parameters, by the share of the landings that carry one", ids(EmailParamNames), "assumption"},
		{"LandingValueLengths", "characters of the value of each parameter", lengths(LandingValueLengths), "assumption: the lengths are those the parameters are seen to have, the characters are made up"},
		{"ReturningShare", "shopper visits by a returning visitor, who comes back to a cart of their own", pct(ReturningShare), "assumption, within the published 17% (Top Growth Marketing) to 53% (Contentsquare 2025)"},
		{"ReturningCartLines", "products in the cart of a returning visitor, besides the demonstration cart", strconv.Itoa(ReturningCartLines), "assumption"},
		{"AddPieces", "pieces an add puts in the cart", fmt.Sprintf("%d to %d", AddPieces[0], AddPieces[1]), "assumption"},
		{"LandingShares", "first page of a shopper visit", strings.Join(landing, ", "), "assumption"},
		{"LanguageShares", "language of a shopper visit", shares(LanguageShares), "assumption"},
		{"ProductZipf", "popularity of the products", fmt.Sprintf("Zipf, exponent %.1f, by rank in the catalogue", ProductZipf), "inference: sales follow a power law (Brynjolfsson, Hu and Simester, Management Science 2011)"},
		{"StoreClicks", "links a shopper clicks, by the kind of page and of link: relative weights, made shares only among the links the page has, and among its products by popularity", clicks(StoreClicks), "assumption"},
		{"StaffShare", "visits made by the staff in the back office", pct(StaffShare), "assumption"},
		{"StaffViewsMean", "page views of a staff visit on average", strconv.Itoa(StaffViewsMean), "assumption"},
		{"StaffViewsMax", "most page views of a staff visit", strconv.Itoa(StaffViewsMax), "assumption"},
		{"StaffViewsMore", "chance a staff visit goes on to one more page, in the first draw of the lengths", pct(StaffViewsMore), "assumption"},
		{"StaffLandingShares", "first page of a staff visit (hub: a list or a stats page)", strings.Join(staffLanding, ", "), "assumption"},
		{"StaffClicks", "links a member of staff clicks, by the kind of page and of link: relative weights, made shares only among the links the page has", clicks(StaffClicks), "assumption"},
		{"StaffEditShare", "staff visits that edit a product", pct(StaffEditShare), "assumption"},
		{"StaffShipShare", "staff visits that ship an order", pct(StaffShipShare), "assumption"},
		{"EditPriceShare", "edits of a product that set its price (the others set its stock)", pct(EditPriceShare), "assumption"},
		{"EditPriceFen", "price an edit sets", fmt.Sprintf("¥%d to ¥%d, in whole yuan", EditPriceFen[0]/100, EditPriceFen[1]/100), "assumption"},
		{"EditDelivery", "pieces an edit of the stock sets above what the restock gave the product", fmt.Sprintf("%d to %d", EditDelivery[0], EditDelivery[1]), "assumption"},
		{"SearchZipf", "exponent of the popularity of the search words of a language, ranked in the order the catalogue first has them", strconv.FormatFloat(SearchZipf, 'f', -1, 64),
			"assumption: the searches of a shop follow a power law with a long tail (Best Buy: over 80% of the distinct queries searched 3 times or fewer in 6 months, arXiv 2505.01946, https://arxiv.org/html/2505.01946v1), and no exponent was found"},
		{"SearchTermsMin", "fewest search words of a language, made from the catalogue (the names, the tags, the materials, the categories and the colours), and filled to this number with misspellings that find nothing", searchTermCounts(SearchTermsMin), "assumption for a shop of 150 products: no data on the distinct words a small shop is searched for"},
		{"SearchTermsMax", "most search words of a language", strconv.Itoa(SearchTermsMax), "assumption, as above"},
		{"SearchNoResultShare", "searches that find nothing, whatever the share of the words of a list that find nothing: the class of a search is dealt by this quota, and its word is drawn within the class", pct(SearchNoResultShare),
			"assumption: 10% to 15% is the figure most often given (Hello Retail, https://helloretail.com/en/blog/2026-02-24-ecommerce-search-statistics/, which names no source), with no sample that can be checked. The low end, for a small catalogue"},
		{"SearchUniqueShare", "searches for a word that no other visit searches for, which finds nothing: they are among the searches that find nothing, and the rest of those search for a word of the list", pct(SearchUniqueShare),
			"assumption, from one account of a shop where random tail queries with numbers were 5% of the searches (an article of Ecommerce-Nation about one client, read in a summary only)"},
		{"SearchUniqueLength", "characters of a search word of a visit's own, lower case letters and digits", strconv.Itoa(SearchUniqueLength), "assumption: no data on the shape of such words"},
		{"SearchShare", "shopper visits that search, once, with the search box of the header", pct(SearchShare),
			"assumption below the one behavioural figure found: searchers are 24% of the visitors of 113 retail sites, October to December 2024 (Constructor, \"Beyond Relevance\", March 2025, as reported by https://itbrief.co.uk/story/online-shopper-search-drives-44-of-ecommerce-revenue), taken lower for a shop of 150 products. One search a visit is an assumption too"},
		{"ListFilterShare", "shopper visits that filter the list, of those that see a product list", pct(ListFilterShare), "assumption: the published ranges for the use of filters (20% to 60%) name no source that can be checked"},
		{"FilterConditionShares", "filters that set one, two and three conditions", sharesOf(FilterConditionShares), "assumption: no data on how many filters a visit uses"},
		{"FilterKinds", "kinds of condition of a filter, as relative weights. A condition on the price is one of the four price bands of the page, never a range typed by hand", filterKinds(), "assumption: no data on which filters are used most, and the order is a judgment"},
		{"ListSortShare", "shopper visits that change the sort of the list, of those that see a product list, to one of the other sorts, all alike", pct(ListSortShare), "assumption: nothing was found on sorting"},
		{"ListPageShares", "shopper visits that stay on the first page, go to the second and go on to the third, of those that see a product list of more than one page with no search term", sharesOf(ListPageShares),
			"Luigi's Box, 4,000 shops: 88.7% of the customers stay on the first page of search results, 9.7% go to the second and 1.6% deeper (https://www.luigisbox.com/blog/product-ranking-data/, with no date and no method), rounded. The figure is of search results, and it is used on the lists with no search term because a search of this shop never has a second page"},
		{"CrawlerShare", "crawler requests among the HTML requests (the documents the browsers of shoppers and staff ask for, which are every landing and every action the page does as a document load, counted from the traces, and the crawler requests), each one GET of a document with no cookie that runs no JavaScript, apart from the visits and the page views of people", pct(CrawlerShare), "Cloudflare Radar, 2025 year in review (https://blog.cloudflare.com/radar-2025-year-in-review/): Googlebot alone is 4.5% of the HTML requests and the other AI crawlers together 4.2%, about 9%"},
		{"CrawlerClasses", "crawlers by class, as relative weights (the total is fixed at the share above): the search class is Googlebot alone, the AI class the other AI crawlers", crawlerClasses(), "Cloudflare Radar, 2025 year in review (https://blog.cloudflare.com/radar-2025-year-in-review/): Googlebot 4.5% and the other AI crawlers 4.2% of the HTML requests, used here as relative weights. The report has no data on the other search engine crawlers, so none is in the mix"},
		{"CrawlerAgents", "the crawlers of each class with their share in it, each sending its own User-Agent", crawlerAgents(), "assumption: the two User-Agents of Googlebot, half each, and the names and the shares of the AI crawlers"},
		{"CrawlOldShare", "crawler requests for an old URL that answers 404, the others for a public page, all alike", pct(CrawlOldShare), "assumption: crawlers ask for old URLs that people no longer visit (Screaming Frog, https://www.screamingfrog.co.uk/what-i-learnt-from-analysing-7-million-log-file-events/), with no figure to take"},
		{"OldProductsPerLanguage", "old URLs of products taken off the shop in the set a crawler asks 404 pages from, in each language", strconv.Itoa(OldProductsPerLanguage), "assumption"},
		{"OldPostsPerLanguage", "old URLs of posts that were moved or deleted in the set a crawler asks 404 pages from, in each language", strconv.Itoa(OldPostsPerLanguage), "assumption"},
		{"ShopperBlockVisits", "shopper visits of a block of the plans, which has the mix exactly", strconv.Itoa(ShopperBlockVisits), "a choice of the method"},
		{"StaffBlockPlans", "different staff visits recorded for each block", strconv.Itoa(StaffBlockPlans), "a choice of the method"},
	}
}
