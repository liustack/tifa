package compare

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// BrowseScenario is one traffic mix of the site: shopper and staff visits on the same server. What a
// shopper visit does is the plan's (mix.go, the same in every scenario). Staff is the share of the
// visits made by the staff, and Edit and Ship the shares of the staff visits that edit a product and
// that ship an order. Throughput says that the scenario has the closed tables, and Latency the open
// run (the search for the rate of Next.js and every server at half of it). ReadOnly is the read-only
// control: the same visits with no writes.
type BrowseScenario struct {
	Name       string  `json:"name"`
	Title      string  `json:"title"`
	Staff      float64 `json:"staffShare"`
	Edit       float64 `json:"staffEditShare"`
	Ship       float64 `json:"staffShipShare"`
	Throughput bool    `json:"throughput"`
	Latency    bool    `json:"latency"`
	ReadOnly   bool    `json:"readOnly,omitempty"`
	// Crawler is the share of the HTML requests that crawlers make (CrawlerShare), 0 for a scenario with
	// none. A block of the cycle then holds, among its visits, the crawlerSlots requests of crawlers that
	// go with the documents its visits ask for.
	Crawler float64 `json:"crawlerShare,omitempty"`
}

// staffVisits is how many staff visits a cycle plays for each block of shopper plans: the share
// Staff of all the visits, rounded to a whole number.
func (s BrowseScenario) staffVisits() int {
	return int(math.Round(ShopperBlockVisits * s.Staff / (1 - s.Staff)))
}

// blockVisits is the visits of a cycle for each block of plans: every shopper plan of the block once
// and the staff visits.
func (s BrowseScenario) blockVisits() int { return ShopperBlockVisits + s.staffVisits() }

// crawlerSlots is how many crawler requests go with docs documents that the browsers of people ask
// for. The HTML requests are those documents and the crawler requests, so crawlers are Crawler of them
// when there are docs * Crawler / (1 - Crawler) crawler requests, rounded to a whole number.
func (s BrowseScenario) crawlerSlots(docs int) int {
	if s.Crawler <= 0 {
		return 0
	}
	return int(math.Round(float64(docs) * s.Crawler / (1 - s.Crawler)))
}

// viewsPerVisit is the page views a visit of the scenario plans on average, by the mix.
func (s BrowseScenario) viewsPerVisit() float64 {
	return (ShopperViewsMean*ShopperBlockVisits + StaffViewsMean*float64(s.staffVisits())) / float64(s.blockVisits())
}

// openBase is the server whose sustainable rate sets the rate of an open run: half of it, for every
// server of the scenario.
const openBase = "nextjs"

// BrowseScenarios are the scenarios of the browse comparison: one, the shop with the mix of mix.go,
// shoppers and staff on the same server.
var BrowseScenarios = []BrowseScenario{
	{Name: "shop", Title: "the shop: shoppers and staff", Staff: StaffShare, Edit: StaffEditShare, Ship: StaffShipShare, Crawler: CrawlerShare, Throughput: true, Latency: true},
}

// BrowseScenarioByName finds a scenario of the browse comparison.
func BrowseScenarioByName(name string) (BrowseScenario, bool) {
	for _, s := range BrowseScenarios {
		if s.Name == name {
			return s, true
		}
	}
	return BrowseScenario{}, false
}

// maxConns is how many connections a browser opens to one host (HTTP/1.1): a visit never has more
// requests in flight than this, whatever the framework.
const maxConns = 6

// ActionSpacing is the time between the planned actions of a visit in the open model.
const ActionSpacing = time.Second

// A Workload is what is replayed for one framework and one scenario: the plans and their traces, and
// the order visits play them in. A run plays visits in the order of one sequence of cycles. A cycle
// plays every shopper plan once, block by block, and among the shopper visits of each block the
// staff visits of the scenario (BrowseScenario.staffVisits) at places of a seeded shuffle. The k-th
// staff visit of the run plays the staff plans in turn, so the cycles play other staff plans. A
// cycle has the mix exactly, and a round plays whole cycles.
//
// The writes of a shopper visit are the plan's, with what they put in the cart drawn for each visit
// (storeWritesOf). Whether a staff visit edits a product, ships an order or only reads is dealt by
// quota among the staff visits of each block of a cycle, and the page view the write follows is drawn
// for each visit.
//
// A workload made by hand with no cycle (the tests' own) plays its plans in order, with the writes of
// Writes, by plan and page view.
type Workload struct {
	Plans    []Plan
	Traces   map[int]Trace // by plan ID
	Writes   map[[2]int]PlannedWrite
	Scenario BrowseScenario
	Seed     uint64       // the seed the writes are drawn from
	crawlers *crawlLayout // the slots of crawler requests among the visits of a cycle, nil for none
	cycle    []slot
	staff    []int // the indexes in Plans of the staff plans
	perCycle int   // the staff visits of a cycle
}

// slot is a place of a cycle: a shopper plan (its index in Plans), or the staff visit k of the cycle
// and the write it makes (WriteEdit, WriteShip or none).
type slot struct {
	plan  int
	staff int
	kind  string
}

// visit is the plan that visit seq of the run plays, and the write of a staff visit.
func (w *Workload) visit(seq int) (Plan, slot) {
	if len(w.cycle) == 0 {
		return w.Plans[seq%len(w.Plans)], slot{plan: -1}
	}
	s := w.cycle[seq%len(w.cycle)]
	if s.plan >= 0 {
		return w.Plans[s.plan], s
	}
	k := seq/len(w.cycle)*w.perCycle + s.staff
	return w.Plans[w.staff[k%len(w.staff)]], s
}

// planFor is the plan that visit seq of the run plays.
func (w *Workload) planFor(seq int) Plan {
	p, _ := w.visit(seq)
	return p
}

// cycleLen is the number of visits of a cycle.
func (w *Workload) cycleLen() int {
	if len(w.cycle) == 0 {
		return len(w.Plans)
	}
	return len(w.cycle)
}

// blockVisits is the visits of a cycle for each block of plans.
func (w *Workload) blockVisits() int {
	if len(w.cycle) == 0 {
		return len(w.Plans)
	}
	return w.Scenario.blockVisits()
}

// plan sets in a round what visits seq0 to seq0+n-1 of the run plan: the visits by role, their page
// views, the clicks among them, the documents their browsers ask for, the returning visitors, and the
// writes.
func (w *Workload) plan(r *Round, seq0, n int) {
	r.VisitsPlanned, r.PageViewsPlanned, r.ClicksPlanned, r.WritesPlanned = n, 0, 0, 0
	r.Crawler = nil
	// A workload made by hand has no mix to check: its counts are made and dropped.
	m := &PlannedMix{}
	r.Mix = nil
	if len(w.cycle) > 0 {
		r.Mix = m
	}
	if c := w.crawlersIn(seq0, n); c > 0 {
		m.Crawlers = c
		r.Crawler = &CrawlStats{Planned: c}
	}
	for s := seq0; s < seq0+n; s++ {
		p := w.planFor(s)
		var c PlanCounts
		c.Add(p)
		r.PageViewsPlanned += c.Views
		r.ClicksPlanned += c.Clicks
		m.Shoppers, m.Staff, m.ShopperViews, m.Returning = m.Shoppers+c.Shoppers, m.Staff+c.Staff, m.ShopperViews+c.ShopperViews, m.Returning+c.Returning
		m.ClickIDs, m.EmailParams = m.ClickIDs+c.ClickIDs, m.EmailParams+c.EmailParams
		// NewWorkload made sure every plan of a cycle has the trace of its page views. A workload made by
		// hand may have none, and has no mix.
		if docs, err := w.docSteps(p); err == nil {
			m.Documents += countTrue(docs)
		}
		for _, x := range w.writesOf(s) {
			r.WritesPlanned++
			switch x.Kind {
			case WriteAdd:
				m.Adds++
			case WriteCheckout:
				m.Checkouts++
			case WriteEdit:
				m.Edits++
			case WriteShip:
				m.Ships++
			}
		}
	}
}

// NewWorkload lays out the cycle of a scenario over the plans of the site, and fails if a plan has no
// trace, or a trace that has not its page views: the trace says which page views are documents
// (docSteps), which the crawler requests of a block are counted from (blockDocuments).
func NewWorkload(set *PlanSet, traces map[int]Trace, sc BrowseScenario, seed uint64) (*Workload, error) {
	w := &Workload{Plans: set.Plans, Traces: traces, Scenario: sc, Seed: seed}
	for _, p := range set.Plans {
		if _, ok := traces[p.ID]; !ok {
			return nil, fmt.Errorf("plan %d has no trace", p.ID)
		}
	}
	if len(set.Plans) != set.Blocks*SiteBlockPlans {
		return nil, fmt.Errorf("%d plans for %d blocks of %d", len(set.Plans), set.Blocks, SiteBlockPlans)
	}
	docs, err := w.blockDocuments(set.Blocks)
	if err != nil {
		return nil, err
	}
	staff := sc.staffVisits()
	k := 0
	for b := range set.Blocks {
		rng := rand.New(rand.NewPCG(seed^0x1f83d9abfb41bd6b, uint64(b)))
		kinds := quota(rng, staff, []string{WriteEdit, WriteShip, ""}, []float64{sc.Edit, sc.Ship, 1 - sc.Edit - sc.Ship})
		at := map[int]bool{}
		for _, i := range rng.Perm(ShopperBlockVisits + staff)[:staff] {
			at[i] = true
		}
		shopper, st := b*SiteBlockPlans, 0
		for i := range ShopperBlockVisits + staff {
			if at[i] {
				w.cycle = append(w.cycle, slot{plan: -1, staff: k, kind: kinds[st]})
				k++
				st++
			} else {
				w.cycle = append(w.cycle, slot{plan: shopper})
				shopper++
			}
		}
		for i := range StaffBlockPlans {
			w.staff = append(w.staff, b*SiteBlockPlans+ShopperBlockVisits+i)
		}
	}
	w.perCycle = k
	if sc.Crawler > 0 {
		per := make([]int, set.Blocks)
		for b := range per {
			per[b] = sc.crawlerSlots(docs[b])
		}
		w.crawlers = newCrawlLayout(seed, sc.blockVisits(), per)
	}
	return w, nil
}

// blockDocuments counts, for each block of a cycle, the documents the browsers of its visits ask for:
// for each shopper plan of the block, played once a cycle, its landing and every action the page did
// as a document load, as the trace of the plan has them (docSteps), and one for each staff visit. The
// traces of every framework agree on them (LoadTraces), so the count is the same for every server.
//
// The staff visits of a block play other staff plans in every cycle, while the crawler requests of a
// block are laid out once for all cycles. So a scenario with crawlers and staff takes a staff visit
// for one document, its landing, and refuses a staff plan whose trace has it otherwise (a landing that
// is no document, or a later page view that is one): the pages of the back office have no plain link
// among those a plan clicks.
func (w *Workload) blockDocuments(blocks int) ([]int, error) {
	staff := w.Scenario.staffVisits()
	docs := make([]int, blocks)
	for b := range docs {
		for i, p := range w.Plans[b*SiteBlockPlans : (b+1)*SiteBlockPlans] {
			steps, err := w.docSteps(p)
			if err != nil {
				return nil, err
			}
			switch n := countTrue(steps); {
			case i < ShopperBlockVisits:
				docs[b] += n
			case (n != 1 || !steps[0]) && staff > 0 && w.Scenario.Crawler > 0:
				return nil, fmt.Errorf("staff plan %d has the documents %v by page view: the crawler requests of a block are counted with one document for each staff visit, its landing", p.ID, steps)
			}
		}
		docs[b] += staff
	}
	return docs, nil
}

// countTrue is how many of list are true.
func countTrue(list []bool) int {
	n := 0
	for _, x := range list {
		if x {
			n++
		}
	}
	return n
}

// runner plays a workload against one server.
type runner struct {
	base       string
	write      Writer
	create     func(project string) string // the path that places an order in a project
	storeWrite func(path string) string    // the path in this app of a write of the storefront
	// products and editProduct are the paths of the product list of the back office and of the
	// edit of a product (Spec.Products, Spec.EditProduct).
	products    func(project, buildID string) string
	editProduct func(project, product string) string
	buildID     string // of Tifa, put into the navigation requests it recorded
	// metrics is the path of the metrics route of Tifa (Spec.Metrics), "" for a server without one.
	metrics string
	wl      *Workload
	seed    uint64
	// inflight counts the requests sent and not yet answered, for the check that no backlog grows.
	inflight atomic.Int64
	cpu      func() float64 // processor seconds of the server so far
	cpuKnown bool           // cpu reads them (ReadsCPU): it is 0 where they are not read
	rss      func() int64   // its peak resident size in bytes
	writes   atomic.Int64   // the number of writes made, which gives each rename its address
	pool     pool           // the orders that staff writes go to, kept as long as the server lives
	// carts are the cookies of the returning visits of the round, by visit, made by setupCarts before
	// the clock.
	carts map[int]http.CookieJar
	// prepared is how many visits from the first the data before the clock is made for, besides the
	// warm-up: the restock, the orders of the ships and the carts of the returning visitors
	// (browse.with). It is at least the visits the run plays, and the same for every server of a
	// scenario. warm is how many visits the warm-up before the clock plays, the ones after those
	// (visits prepared to prepared+warm-1, warmUp), and the data is made for them too: 0 for a run
	// with no warm-up (a sizing run).
	prepared, warm int
	// deps are the products each page shows (store_deps.json), which the expected hit rate of a round of Tifa
	// is worked out from, and expect is that count for the round being played: the class of each document.
	deps   *StoreDeps
	expect *Expectation
	// restocked is the stock the restock gave each product of the storefront it raised, by slug, set
	// before the clock (prepare). An edit of the stock sets more than that (productChange).
	restocked map[string]int
}

var tifaBuild = regexp.MustCompile(`_tifa=[0-9a-f]+\.`)

// acc collects what a run saw, as it happens: a page view when it completes, a request when it
// completes, a write when it is answered.
type acc struct {
	mu                                  sync.Mutex
	visits, views, loads, clicks, wrote int
	// started counts the visits begun, and failedVisits the ones an action of which failed.
	started, failedVisits int
	// cutOff counts the actions in flight when the round was cut off, cutLate the ones in flight for
	// longer than the rule, and cutOldest how long the oldest was, in ms.
	cutOff, cutLate                     int
	cutOldest                           float64
	failures, requests, cancelled, n304 int
	bytes                               int64
	firstErr                            string
	pages                               map[string]int
	renders, responses                  map[string]int
	lat                                 map[string][]float64
	samples                             []viewSample // every page view that completed, for the series
	lastAction                          time.Time
	// kinds counts the writes by kind.
	kinds map[string]int
	// refused counts the requests the shop refused with 409 or 422 (see refuse).
	refused int
	// pageRenders counts the renders of documents and navigation data (X-Tifa-Render) by kind of page.
	pageRenders map[string]map[string]int
	// encodings counts the answers of the page views by their Content-Encoding (identity for none).
	encodings map[string]int
	// seen holds the URLs Tifa has answered in the round (X-Tifa-Render), and vmWhy counts the answers it
	// rendered in a VM by kind of page and why (renderWhy).
	seen  map[string]bool
	vmWhy map[string]map[string]int
	// crawl counts what the crawler requests of the run did (crawler.go).
	crawl crawlCounts
	// landing counts how Tifa answered the documents of the landings of the storefront (X-Tifa-Render), by
	// the parameter the URL carries (landingGroup).
	landing map[string]map[string]int
}

// refuse counts an answer with the status status as refused when it is 409 or 422. After the
// restock, the plan makes every write one the shop takes and reads only pages that are there, so
// such an answer means that the round is not the experiment it was meant to be (Round.Refused).
func (a *acc) refuse(status int) {
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		a.mu.Lock()
		a.refused++
		a.mu.Unlock()
	}
}

func newAcc() *acc {
	return &acc{pages: map[string]int{}, renders: map[string]int{}, responses: map[string]int{}, lat: map[string][]float64{}, kinds: map[string]int{}, pageRenders: map[string]map[string]int{}, encodings: map[string]int{}, seen: map[string]bool{}, vmWhy: map[string]map[string]int{}, landing: map[string]map[string]int{}, crawl: crawlCounts{statuses: map[string]int{}, renders: map[string]map[string]int{}, why: map[string]map[string]int{}}}
}

// fail counts a visit that failed, and the error that ended it.
func (a *acc) fail(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	a.failedVisits++
	if a.firstErr == "" {
		a.firstErr = err.Error()
	}
}

// cut counts an action that the end of the round cut off, scheduled at sched: how long it had been in
// flight when the context ended (at its deadline, when it has one that passed).
func (a *acc) cut(ctx context.Context, sched time.Time) {
	at := time.Now()
	if d, ok := ctx.Deadline(); ok && d.Before(at) {
		at = d
	}
	age := ms(at.Sub(sched))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cutOff++
	if age > LatencyLimit {
		a.cutLate++
	}
	a.cutOldest = max(a.cutOldest, age)
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// visitState is what one visit keeps: cookies, the ETags it got, the connections it may use and its
// background work.
type visitState struct {
	client *http.Client
	// seq is the number of the visit in the sequence of visits.
	seq int
	// landing is what the visit puts in place of the placeholder of its landing parameter in every
	// request it sends (landingSub).
	landing landingSub
	sem     chan struct{}
	etagMu  sync.Mutex
	etags   map[string]string // by URL
	bg      sync.WaitGroup
	errMu   sync.Mutex
	err     error // the first background request that failed
}

func (v *visitState) fail(err error) {
	v.errMu.Lock()
	defer v.errMu.Unlock()
	if v.err == nil {
		v.err = err
	}
}

func (v *visitState) failure() error {
	v.errMu.Lock()
	defer v.errMu.Unlock()
	return v.err
}

// etag is the ETag of the last answer the visit got for a URL, for a request that the browser sent
// with If-None-Match.
func (v *visitState) etag(url string) string {
	v.etagMu.Lock()
	defer v.etagMu.Unlock()
	return v.etags[url]
}

func (v *visitState) setETag(url, e string) {
	v.etagMu.Lock()
	defer v.etagMu.Unlock()
	if v.etags == nil {
		v.etags = map[string]string{}
	}
	v.etags[url] = e
}

// outcome of one request.
type outcome struct {
	end       time.Time
	cancelled bool // the browser cancelled it, and the replay did too
	responded bool // the headers of the answer had arrived: a request cancelled after that was used
	err       error
}

var errNotSent = errors.New("cancelled before it was sent")

// send plays one recorded request. A request the browser cancelled is cancelled at the recorded
// offset from the start of its step: the connection is released and the rest of the answer is not
// read. cancelAt is that time (zero for a request the browser let finish). page is the page of the
// page view the request belongs to, and step the number of that page view in the visit.
func (r *runner) send(ctx context.Context, v *visitState, a *acc, q TraceReq, step int, page string, cancelAt time.Time) outcome {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	responded, sent := false, false
	cancelled := func(err error) outcome {
		// Our own timer cancelled it, and the visit itself is still going.
		if ctx.Err() == nil && rctx.Err() != nil && !cancelAt.IsZero() {
			if sent {
				// The server got it and did what it does until the cancel: count it as a request.
				a.mu.Lock()
				a.requests++
				a.responses[q.Class+" cancelled"]++
				a.mu.Unlock()
			}
			return outcome{end: time.Now(), cancelled: true, responded: responded}
		}
		return outcome{end: time.Now(), err: err}
	}
	url := q.URL
	if r.buildID != "" {
		url = tifaBuild.ReplaceAllString(url, "_tifa="+r.buildID+".")
	}
	url = v.landing.apply(url)
	var body io.Reader
	if q.Body != "" {
		body = strings.NewReader(v.landing.apply(q.Body))
	}
	req, err := http.NewRequestWithContext(rctx, q.Method, r.base+url, body)
	if err != nil {
		return outcome{end: time.Now(), err: err}
	}
	for k, val := range q.Headers {
		req.Header.Set(k, v.landing.apply(val)) // a router carries the URL of the page in a header too
	}
	req.Header.Set("Accept-Encoding", "gzip")
	if q.INM {
		if e := v.etag(q.URL); e != "" {
			req.Header.Set("If-None-Match", e)
		}
	}
	if q.Class != "doc" {
		// What a browser adds to a fetch of its own page. TanStack Start refuses a server function
		// call without it (403).
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
	}
	select {
	case v.sem <- struct{}{}:
	case <-rctx.Done():
		return cancelled(errNotSent)
	}
	defer func() { <-v.sem }()
	if rctx.Err() != nil {
		return cancelled(errNotSent)
	}
	// A request the browser cancelled after the headers of its answer had arrived (Next.js does it to
	// its prefetches and to many of its clicks, once its router has what it needs) is cancelled the
	// same time after the headers arrive. One it cancelled before any answer is cancelled at the
	// recorded offset from the start of the page view, but never sooner than it lived in the browser
	// after it was sent: a replay that is late (a slow server) still sends what the browser sent, and
	// so does the work.
	afterAnswer := !cancelAt.IsZero() && q.Responded > 0 && q.Responded <= q.End
	if !cancelAt.IsZero() && !afterAnswer {
		if least := time.Now().Add(time.Duration((q.End - q.Start) * float64(time.Millisecond))); cancelAt.Before(least) {
			cancelAt = least
		}
		t := time.AfterFunc(time.Until(cancelAt), cancel)
		defer t.Stop()
	}
	// A private page of Tifa rendered in a VM is counted by whether the visit sent a cart cookie with it.
	kind := pageClass(page)
	cart := privateKind(kind) && hasCookie(v.client.Jar, req.URL, StoreCartCookie)
	start := time.Now()
	sent = true
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	res, err := v.client.Do(req)
	if err != nil {
		return cancelled(err)
	}
	responded = true
	a.refuse(res.StatusCode)
	// The answer is judged by its status as soon as its headers arrive, whether or not the request is
	// cancelled later: a 500 whose body the cancel cuts off is still a failure. A 204 is the answer
	// of a recorded POST of the storefront.
	if res.StatusCode != 200 && res.StatusCode != 204 && res.StatusCode != 304 {
		res.Body.Close()
		return outcome{end: time.Now(), err: fmt.Errorf("%s %s: status %d", q.Method, q.URL, res.StatusCode)}
	}
	if afterAnswer {
		t := time.AfterFunc(time.Duration((q.End-q.Responded)*float64(time.Millisecond)), cancel)
		defer t.Stop()
	}
	n, err := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	end := time.Now()
	// The bytes read before a cancel or an error were read by the client too.
	a.mu.Lock()
	a.bytes += n
	a.mu.Unlock()
	if err != nil {
		return cancelled(err)
	}
	if e := res.Header.Get("ETag"); e != "" && res.StatusCode == 200 {
		v.setETag(q.URL, e)
	}
	a.mu.Lock()
	a.requests++
	if res.StatusCode == 304 {
		a.n304++
	}
	a.responses[q.Class+" "+strconv.Itoa(res.StatusCode)+" "+mediaType(res.Header.Get("Content-Type"))]++
	a.encodings[cmp.Or(res.Header.Get("Content-Encoding"), "identity")]++
	if rd := res.Header.Get("X-Remix-Reload-Document"); rd != "" {
		a.responses["reload-document header"]++
	}
	// Tifa says how it answered a page: a document, and also the navigation data of a page, which can
	// render the page in a VM too (one with Cache). Every answer that says so is counted,
	// by the kind of page of the page view.
	if rd := res.Header.Get(RenderHeader); rd != "" && rd != RenderNone {
		a.renders[rd]++
		if a.pageRenders[kind] == nil {
			a.pageRenders[kind] = map[string]int{}
		}
		a.pageRenders[kind][rd]++
		// A landing parameter is no part of the page: the URL it asks for is the one without it.
		seenAt := stripLanding(q.URL)
		seen := a.seen[seenAt]
		a.seen[seenAt] = true
		// The table of the landings has the first document of a visit, not one a later action asks for.
		if q.Class == "doc" && step == 0 && isStorePage(page) {
			g := landingGroup(q.URL)
			if a.landing[g] == nil {
				a.landing[g] = map[string]int{}
			}
			a.landing[g][rd]++
		}
		if rd == RenderVM {
			if a.vmWhy[kind] == nil {
				a.vmWhy[kind] = map[string]int{}
			}
			why := renderWhy(kind, seen, cart)
			// A document the plans counted has the class they give it, by the visit and the page view.
			if class, ok := r.expect.humanClass(v.seq, step); ok && q.Class == "doc" && !privateKind(kind) {
				why = docWhy(class)
			}
			a.vmWhy[kind][why]++
		}
	}
	if q.Class == "prefetch" {
		a.lat["prefetch"] = append(a.lat["prefetch"], ms(end.Sub(start)))
	}
	a.mu.Unlock()
	return outcome{end: end, responded: true}
}

// runStep plays the requests of one page view with the order and the parallelism the browser had: a
// request starts when every request that had finished (or had been cancelled) before it started in
// the browser is done. It returns when the foreground requests are done; prefetches go on in the
// background until the end of the visit. scheduled is when the action was meant to start, and the
// latency is counted from it. step is the number of the page view in the visit. It returns the time of
// the end of the page view, and whether it was successful: no foreground request failed and at least
// one finished.
func (r *runner) runStep(ctx context.Context, v *visitState, a *acc, step int, s TraceStep, scheduled time.Time) (time.Time, error) {
	stepStart := time.Now()
	n := len(s.Reqs)
	done := make([]chan struct{}, n)
	for i := range done {
		done[i] = make(chan struct{})
	}
	outs := make([]outcome, n)
	var main sync.WaitGroup
	for j := range s.Reqs {
		q := s.Reqs[j]
		var deps []chan struct{}
		for i := range s.Reqs {
			// A request that started in the same millisecond is no one's dependency, so that two quick
			// requests cannot wait for each other.
			if i != j && s.Reqs[i].End <= q.Start && s.Reqs[i].Start < q.Start {
				deps = append(deps, done[i])
			}
		}
		var cancelAt time.Time
		if q.Cancel != "" {
			cancelAt = stepStart.Add(time.Duration(q.End * float64(time.Millisecond)))
		}
		wg := &main
		if q.Class == "prefetch" {
			wg = &v.bg
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(done[j])
			for _, d := range deps {
				select {
				case <-d:
				case <-ctx.Done():
					return
				}
			}
			outs[j] = r.send(ctx, v, a, q, step, s.Page, cancelAt)
			if q.Class == "prefetch" && outs[j].err != nil && ctx.Err() == nil {
				v.fail(outs[j].err) // a prefetch that fails is a failure of the visit
			}
		}()
	}
	main.Wait()
	if ctx.Err() != nil {
		return time.Time{}, ctx.Err()
	}
	var last time.Time
	finished := 0
	for j := range outs {
		if s.Reqs[j].Class == "prefetch" {
			continue
		}
		o := outs[j]
		if o.err != nil {
			return time.Time{}, o.err
		}
		if !o.cancelled || o.responded {
			finished++
		}
		if o.cancelled {
			a.mu.Lock()
			a.cancelled++
			a.mu.Unlock()
		}
		if o.end.After(last) {
			last = o.end
		}
	}
	if finished == 0 {
		return time.Time{}, fmt.Errorf("the page view of %s has no foreground request that finished", s.Page)
	}
	kind := "click"
	if s.Kind == StepLoad {
		kind = "load"
	}
	class := pageClass(s.Page)
	a.mu.Lock()
	a.lat[kind] = append(a.lat[kind], ms(last.Sub(scheduled)))
	a.samples = append(a.samples, viewSample{last, ms(last.Sub(scheduled))})
	a.views++
	if kind == "load" {
		a.loads++
	} else {
		a.clicks++
	}
	a.pages[class]++
	a.mu.Unlock()
	return last, nil
}

// pacer says when each action of a visit is meant to start. In the closed model an action starts
// when the one before it is done, so it is scheduled when it starts. In the open model action j of a
// visit that starts at t is scheduled at t + j * ActionSpacing, whatever happened before.
type pacer struct {
	open  bool
	start time.Time
}

func (p pacer) at(j int) time.Time {
	if p.open {
		return p.start.Add(time.Duration(j) * ActionSpacing)
	}
	return time.Now()
}

// runVisit plays plan number seq of the global sequence: its page views in turn, the writes of the
// scenario after the page views the plan puts them after, and at the end the background requests
// the browser still had. It reports whether the visit completed. A visit that a failed action ends
// counts as failed. One that the context ends (the round was cut off) counts as neither.
func (r *runner) runVisit(ctx context.Context, tr *http.Transport, a *acc, seq int, p pacer) bool {
	plan := r.wl.planFor(seq)
	trace := r.wl.Traces[plan.ID]
	// A returning visitor comes back with the cookies of their cart, everyone else with none.
	var jar http.CookieJar
	if j, ok := r.carts[seq]; ok && plan.Returning {
		jar = j
	} else {
		jar, _ = cookiejar.New(nil)
	}
	v := &visitState{client: &http.Client{Transport: tr, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sem: make(chan struct{}, maxConns), landing: landingFor(r.seed, seq, plan), seq: seq}
	// Every way out of the visit waits for its background requests, so that a round ends after its
	// last request and a warm-up does not run on into the round.
	defer v.bg.Wait()
	a.mu.Lock()
	a.started++
	a.mu.Unlock()
	action := 0
	wait := func() time.Time {
		sched := p.at(action)
		action++
		if d := time.Until(sched); d > 0 && p.open {
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}
		}
		a.mu.Lock()
		a.lastAction = time.Now()
		a.mu.Unlock()
		return sched
	}
	writes := r.wl.writesOf(seq)
	for i, s := range trace.Steps {
		sched := wait()
		if ctx.Err() != nil {
			return false
		}
		if _, err := r.runStep(ctx, v, a, i, s, sched); err != nil {
			if ctx.Err() == nil {
				a.fail(err)
			} else {
				a.cut(ctx, sched)
			}
			return false
		}
		if w, ok := writes[i]; ok {
			sched := wait()
			if err := r.doWrite(ctx, v, a, w, sched); err != nil {
				if ctx.Err() == nil {
					a.fail(err)
				} else {
					a.cut(ctx, sched)
				}
				return false
			}
		}
	}
	v.bg.Wait()
	if ctx.Err() != nil {
		return false
	}
	if err := v.failure(); err != nil {
		a.fail(err)
		return false
	}
	a.mu.Lock()
	a.visits++
	a.mu.Unlock()
	return true
}

// mediaType is a content type without its parameters.
func mediaType(v string) string {
	if v == "" {
		return "-"
	}
	if m, _, err := mime.ParseMediaType(v); err == nil {
		return m
	}
	return v
}

// selfCPU is the processor time this process has used, in seconds.
func selfCPU() float64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}

// writesOf is the writes that visit seq of the run sends, by the page view they follow. They are
// worked out when the visit starts, not kept: a round of any length needs no table. A shopper visit
// sends the writes of its plan (storeWritesOf). A staff visit that edits or ships makes its write
// after a page view drawn from the seed and seq, in the studio shop, whose orders the storefront
// shows. A read-only scenario sends none.
func (w *Workload) writesOf(seq int) map[int]PlannedWrite {
	out := map[int]PlannedWrite{}
	if w.Scenario.ReadOnly {
		return out
	}
	p, s := w.visit(seq)
	switch {
	case len(w.cycle) == 0:
		for j := range p.Steps {
			if x, ok := w.Writes[[2]int{p.ID, j}]; ok {
				x.Seq = seq
				out[j] = x
			}
		}
	case p.Role == RoleShopper:
		for _, x := range storeWritesOf(w.Seed, seq, p) {
			out[x.After] = x
		}
	case s.kind != "":
		rng := rand.New(rand.NewPCG(w.Seed^0x9b05688c2b3e6c1f, uint64(seq)))
		after := rng.IntN(len(p.Steps))
		out[after] = PlannedWrite{After: after, Seq: seq, Kind: s.kind, Project: "studio"}
	}
	return out
}
