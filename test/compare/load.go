package compare

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// TypeStat is the latencies of one type of action or request, in milliseconds.
type TypeStat struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50Ms"`
	P99 float64 `json:"p99Ms"`
}

// Round is one run of a finite workload against a fresh server: what was scheduled, what was
// achieved and what it cost. Everything in it is counted over one interval, from the first
// scheduled action until the last request has finished (or the timeout), so the drain at the end
// is in the denominators. A page view counts when it completes and a request when it completes.
type Round struct {
	Framework string  `json:"framework"` // the spec id: tifa-nocache is a framework of its own here
	Scenario  string  `json:"scenario"`
	Model     string  `json:"model"`                     // "closed" or "open"
	Sessions  int     `json:"sessions,omitempty"`        // closed: the concurrent sessions, no think time
	Rate      float64 `json:"visitsPerSecond,omitempty"` // open: visits started a second
	Round     int     `json:"round"`
	Blocks    int     `json:"blocks"`

	// The visits of the round: planned (what its workload holds), started by the load generator,
	// succeeded (Visits: every action of it did), failed (an action failed, which ends the visit) and
	// unfinished (the round ended, or was cut off, before they ended, the ones never started too).
	VisitsPlanned    int `json:"visitsPlanned"`
	VisitsStarted    int `json:"visitsStarted"`
	Visits           int `json:"visitsSucceeded"`
	VisitsFailed     int `json:"visitsFailed"`
	VisitsUnfinished int `json:"visitsUnfinished"`
	// What the planned visits hold: page views, the clicks among them, and writes. The mix is checked
	// on these. The ones below were done: a page view and a write count when they complete.
	PageViewsPlanned int `json:"pageViewsPlanned"`
	ClicksPlanned    int `json:"clicksPlanned"`
	WritesPlanned    int `json:"writesPlanned"`
	// Mix is what the planned visits hold by role, which the mix of the scenario is checked on. A
	// workload made by hand (a test's, with no cycle of the site) has no mix, and leaves it nil.
	Mix       *PlannedMix    `json:"mixPlanned,omitempty"`
	PageViews int            `json:"pageViews"`
	FullLoads int            `json:"fullLoads"`
	Clicks    int            `json:"clicks"`
	Writes    int            `json:"writes"`
	Pages     map[string]int `json:"pagesByClass"`

	// The shares that the round achieved, over what completed: a diagnostic. A round whose visits
	// failed achieves another mix than it planned.
	WriteShare    float64 `json:"writeShare"`
	ClickShare    float64 `json:"clickShare"`
	ViewsPerVisit float64 `json:"pageViewsPerVisit"`

	Seconds      float64 `json:"seconds"`      // first scheduled action to last completion
	DrainSeconds float64 `json:"drainSeconds"` // from the last action started to the end
	CPUSeconds   float64 `json:"serverCpuSeconds"`
	ClientCPU    float64 `json:"clientCpuSeconds"`
	PeakRSS      float64 `json:"peakRssMiB"`
	Requests     int     `json:"requests"` // completed, writes included
	Cancelled    int     `json:"cancelledForeground"`
	Bytes        int64   `json:"bytes"`
	N304         int     `json:"notModified"`

	Attempt  int    `json:"attempt,omitempty"`
	Failures int    `json:"failures"`
	FirstErr string `json:"firstError,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
	// CutOff counts the actions (page views and writes) in flight when the round was cut off, CutOffLate
	// the ones among them that had been in flight for longer than the rule allows (LatencyLimit), and
	// CutOffOldestMs how long the oldest had. An action cut off within the rule is a matter of sizing.
	// One cut off past it is a request the server did not answer in time, which fails the rule.
	CutOff         int     `json:"cutOffActions,omitempty"`
	CutOffLate     int     `json:"cutOffOverLimit,omitempty"`
	CutOffOldestMs float64 `json:"cutOffOldestMs,omitempty"`
	// Lag is the largest time a visit started after it was scheduled (open model): the generator
	// itself falling behind.
	LagMs float64 `json:"maxStartLagMs,omitempty"`
	// Backlog says that the visits or the requests in flight kept growing while visits were being
	// offered (open model): the server was not keeping up, even if it caught up after the end.
	Backlog bool `json:"backlogGrowing,omitempty"`
	// GeneratorLimited says that the load generator, not the server, ended the round: it started
	// visits late or used most of its cpus.
	GeneratorLimited bool `json:"generatorLimited,omitempty"`
	// SetupRequests is how many requests made the carts of the returning visitors before the clock, and
	// SetupError why they could not be made, or an answer of them rendered a page.
	SetupRequests int    `json:"setupRequests,omitempty"`
	SetupError    string `json:"setupError,omitempty"`
	// FirstRenders is, by kind of page, how Tifa answered the first answer of that kind in the round
	// (X-Tifa-Render). PrivateHits counts the answers of the private pages, the cart and the checkout,
	// that came from the render memo: with a cart of their own for every visitor they should be few.
	FirstRenders map[string]string `json:"firstRenders,omitempty"`
	PrivateHits  int               `json:"privateHits"`

	// Renders counts the answers of Tifa by how it answered (X-Tifa-Render: vm, memo or shared), the
	// documents and the navigation data alike, and PageRenders splits them by kind of page.
	Renders     map[string]int            `json:"renders,omitempty"`
	PageRenders map[string]map[string]int `json:"pageRenders,omitempty"`
	Responses   map[string]int            `json:"responses,omitempty"`
	Types       map[string]TypeStat       `json:"types"`
	// Series is page views a second and their P99 in steps of SeriesLength from the first request: how the
	// round went as the cache filled or the JIT warmed.
	Series []SeriesStep `json:"series,omitempty"`

	// WriteKinds counts the writes by kind. PoolEmpty counts the admin writes that found no order to
	// go to and were sent as a create instead (they are among the creates). Orders is, for each
	// project, how many orders the server held at the end of an admin round: those of the data and
	// every order placed since it started.
	WriteKinds map[string]int `json:"writeKinds,omitempty"`
	PoolEmpty  int            `json:"poolEmpty,omitempty"`
	Orders     map[string]int `json:"orders,omitempty"`
	// Refused counts the requests, writes and pages, that the shop refused with 409 or 422. Given the
	// stock the run was restocked with, the plan makes every write one the shop takes, so a round with
	// one is not valid.
	Refused int `json:"refused,omitempty"`

	// Valid says that the round is a valid experiment: the visits it planned have the mix of its
	// scenario (PlannedMix.why), the carts of the returning visitors were made, no kind of page was first
	// answered from the render memo, the load generator did its work, and a closed round took the length it was
	// sized for or failed the rule. A round that is not valid is run again. A server that fails or falls
	// behind does not make a round invalid: that round is valid, and fails.
	Valid      bool   `json:"valid"`
	WhyInvalid string `json:"whyInvalid,omitempty"`
	// Pass says that every class of action met the rule: P99 of full loads, clicks and writes at
	// most a second, no unexpected failure, no backlog left.
	Pass bool   `json:"pass"`
	Why  string `json:"why,omitempty"`
}

// idleConn is how long the load generator keeps an idle connection. It is shorter than the 5 seconds
// after which Node closes a connection nobody uses, so that it does not send a request on one that is
// being closed (the server resets it) and count a failure that no browser would see.
const idleConn = 3 * time.Second

// LatencyLimit is the P99 that full loads, clicks and writes must meet.
const LatencyLimit = 1000.0

func (a *acc) round(r *Round, cpu0, cpu1, client0, client1 float64, start, end time.Time, rss int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r.Visits, r.PageViews, r.FullLoads, r.Clicks, r.Writes = a.visits, a.views, a.loads, a.clicks, a.wrote
	r.VisitsStarted, r.VisitsFailed = a.started, a.failedVisits
	r.VisitsUnfinished = r.VisitsPlanned - r.Visits - r.VisitsFailed
	r.CutOff, r.CutOffLate, r.CutOffOldestMs = a.cutOff, a.cutLate, a.cutOldest
	r.Pages = a.pages
	r.Failures, r.FirstErr = a.failures, a.firstErr
	r.Requests, r.Cancelled, r.Bytes, r.N304 = a.requests, a.cancelled, a.bytes, a.n304
	r.Renders, r.Responses, r.PageRenders = a.renders, a.responses, a.pageRenders
	if len(a.firstRenders) > 0 {
		r.FirstRenders = map[string]string{}
		for kind, f := range a.firstRenders {
			r.FirstRenders[kind] = f.how
		}
	}
	r.PrivateHits = a.pageRenders[StoreKindCart][RenderHit] + a.pageRenders[StoreKindCheckout][RenderHit]
	r.WriteKinds, r.PoolEmpty = a.kinds, a.poolEmpty
	r.Refused = a.refused
	r.Seconds = end.Sub(start).Seconds()
	if !a.lastAction.IsZero() && a.lastAction.After(start) {
		r.DrainSeconds = end.Sub(a.lastAction).Seconds()
	}
	r.CPUSeconds, r.ClientCPU = cpu1-cpu0, client1-client0
	r.PeakRSS = float64(rss) / (1 << 20)
	r.Series = viewSeries(a.samples, start, end)
	r.Types = map[string]TypeStat{}
	for kind, l := range a.lat {
		sorted := sortedCopy(l)
		r.Types[kind] = TypeStat{N: len(l), P50: Percentile(sorted, 50), P99: Percentile(sorted, 99)}
	}
	if r.PageViews > 0 {
		r.ClickShare = float64(r.Clicks) / float64(r.PageViews)
	}
	if r.PageViews+r.Writes > 0 {
		r.WriteShare = float64(r.Writes) / float64(r.PageViews+r.Writes)
	}
	if r.Visits > 0 {
		r.ViewsPerVisit = float64(r.PageViews) / float64(r.Visits)
	}
}

// Judge sets Valid and Pass of a round. Valid is about the experiment: the visits it planned have
// the mix of the scenario, the carts of the returning visitors were made with answers that render
// nothing, the first answer of every kind of page was rendered (not from the render memo, which
// starts empty), and the load generator started every visit it was meant to (one that fell behind
// or ran out of cpus is found by generatorFault). Pass is about the server, on what
// it did: a server that fails visits or falls behind fails the rule, in a valid round.
func (r *Round) Judge(sc BrowseScenario) {
	r.Valid, r.WhyInvalid = true, ""
	invalid := func(why string) {
		if r.Valid {
			r.Valid, r.WhyInvalid = false, why
		}
	}
	if r.VisitsPlanned == 0 || r.PageViewsPlanned == 0 {
		invalid("no visit planned")
	} else if r.Mix != nil {
		if why := r.Mix.why(sc); why != "" {
			invalid(why)
		}
	}
	if r.SetupError != "" {
		invalid("the carts of the returning visitors were not made: " + r.SetupError)
	}
	for _, kind := range slices.Sorted(maps.Keys(r.FirstRenders)) {
		if r.FirstRenders[kind] == RenderHit {
			invalid(fmt.Sprintf("the first answer of a %s page came from the render memo: something filled it before the round", kind))
		}
	}
	if r.Refused > 0 {
		invalid(fmt.Sprintf("the shop refused %d requests with 409 or 422", r.Refused))
	}
	// A round that was not cut off (by its timeout, or the deadline of the open model) starts every
	// visit it planned: if not, the generator did not do its work.
	if !r.TimedOut && r.VisitsStarted < r.VisitsPlanned {
		invalid(fmt.Sprintf("the load generator started %d of the %d visits planned", r.VisitsStarted, r.VisitsPlanned))
	}
	r.Pass, r.Why = true, ""
	fail := func(why string) {
		if r.Why == "" {
			r.Why = why
		}
		r.Pass = false
	}
	// A closed round cut off by its timeout ran longer than it was sized for, which is a matter of
	// calibration (see calibrate), unless an action it cut off had waited past the rule (below). An
	// open round past its deadline left a backlog.
	if r.TimedOut && r.Model == "open" {
		fail("the workload did not finish in time: a backlog was left")
	}
	// An action that had been in flight for longer than the rule when the round was cut off did not
	// get its answer in time: its latency is over the limit, though it has no sample.
	if r.CutOffLate > 0 {
		fail(fmt.Sprintf("%d actions had been in flight for over %.0f ms when the round was cut off (the oldest for %.0f ms)", r.CutOffLate, LatencyLimit, r.CutOffOldestMs))
	}
	if r.Failures > 0 {
		fail(fmt.Sprintf("%d failures (%s)", r.Failures, r.FirstErr))
	}
	if r.Backlog {
		fail("visits or requests in flight kept growing while visits were offered: a backlog")
	}
	for _, kind := range []string{"load", "click", "write"} {
		t := r.Types[kind]
		if t.N > 0 && t.P99 > LatencyLimit {
			fail(fmt.Sprintf("%s P99 %.0f ms over %.0f ms", kind, t.P99, LatencyLimit))
		}
	}
	if r.Types["load"].N == 0 || r.Types["click"].N == 0 {
		fail("no full load or no click completed")
	}
}

// RunClosed plays visits seq 0..visits-1 of the global sequence with sessions concurrent sessions, each taking the
// next visit when it finishes one (zero think time), and each visit with at most six connections.
// It starts at the first scheduled action and ends when the last visit has finished, or at timeout.
func (r *runner) RunClosed(ctx context.Context, sessions, visits int, timeout time.Duration) *Round {
	round := &Round{Model: "closed", Sessions: sessions, Blocks: visits / r.wl.blockVisits()}
	if !r.setupCarts(ctx, round, 0, visits) {
		r.wl.plan(round, 0, visits, false)
		return round
	}
	a := newAcc()
	var next atomic.Int64
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cpu0, client0 := r.cpu(), selfCPU()
	start := time.Now()
	var wg sync.WaitGroup
	for range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: maxConns, MaxConnsPerHost: maxConns, IdleConnTimeout: idleConn}
			defer tr.CloseIdleConnections()
			for ctx.Err() == nil {
				s := int(next.Add(1)) - 1
				if s >= visits {
					return
				}
				r.runVisit(ctx, tr, a, s, pacer{})
			}
		}()
	}
	wg.Wait()
	end := time.Now()
	round.TimedOut = ctx.Err() == context.DeadlineExceeded
	r.wl.plan(round, 0, visits, false)
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	r.countOrders(round)
	return round
}

// countOrders notes, after a round, how many orders each project of the back office holds.
func (r *runner) countOrders(round *Round) {
	round.Orders = r.pool.orderCounts()
}

// Warm plays the workload with sessions sessions for d, counting nothing, and returns how many visits a
// second it completed. It is the warm-up of a framework, apart from the measured round. It sends no
// writes (see pacer), so the data the round starts with is the same for every framework.
func (r *runner) Warm(ctx context.Context, sessions int, d time.Duration) float64 {
	return r.closedFor(ctx, sessions, d, pacer{reads: true})
}

// Size plays the scenario with its writes for d, on sessions concurrent sessions, and returns the visits a second: what the
// first round of a cell is sized from.
func (r *runner) Size(ctx context.Context, sessions int, d time.Duration) float64 {
	return r.closedFor(ctx, sessions, d, pacer{})
}

// closedFor plays visits on sessions concurrent sessions, each starting the next as soon as one ends, for d, and returns
// the visits a second that ended, completed or failed.
func (r *runner) closedFor(ctx context.Context, sessions int, d time.Duration, p pacer) float64 {
	a := newAcc()
	var next, ended atomic.Int64
	wctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	for range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: maxConns, MaxConnsPerHost: maxConns, IdleConnTimeout: idleConn}
			defer tr.CloseIdleConnections()
			for wctx.Err() == nil {
				// Visits that failed count too (a 503 ends a visit fast): the rate sizes rounds, whose length
				// every visit sets.
				if r.runVisit(wctx, tr, a, int(next.Add(1))-1, p) || wctx.Err() == nil {
					ended.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	return float64(ended.Load()) / time.Since(start).Seconds()
}

// BlocksFor sizes a workload: the whole blocks (a multiple of the number of blocks of the plans,
// at least that many, of blockVisits visits each) that a framework completing rate visits a second
// plays in about target.
func BlocksFor(rate float64, target time.Duration, planBlocks, blockVisits int) int {
	blocks := int(math.Ceil(rate * target.Seconds() / float64(blockVisits) / float64(planBlocks)))
	return max(blocks, 1) * planBlocks
}

// RunOpen starts visits at rate a second, on schedule whether or not the earlier ones are done,
// n visits in all (a whole number of blocks, so that the mix comes out exact). The actions of a
// visit are planned ActionSpacing apart, and latency is counted from the planned time. It ends when
// the last visit has finished, or drain after the last planned action. The visits are the global
// sequence from seq0. With reads set it sends no writes: a warm-up.
func (r *runner) RunOpen(ctx context.Context, rate float64, n int, drain time.Duration, seq0 int, reads bool) *Round {
	round := &Round{Model: "open", Rate: rate}
	if !reads && !r.setupCarts(ctx, round, seq0, n) {
		r.wl.plan(round, seq0, n, reads)
		return round
	}
	a := newAcc()
	duration := time.Duration(float64(n) / rate * float64(time.Second))
	tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: 4096, MaxConnsPerHost: 0, IdleConnTimeout: idleConn}
	defer tr.CloseIdleConnections()
	last := 0 // the longest plan, in actions, for the drain deadline
	for i := range min(n, r.wl.cycleLen()) {
		p := r.wl.planFor(seq0 + i)
		last = max(last, len(p.Steps)+len(r.wl.writesOf(seq0+i)))
	}
	// The visits in flight level off after about the longest visit; the samples before that are left out.
	ramp := int(time.Duration(last)*ActionSpacing/(500*time.Millisecond)) + 2
	cpu0, client0 := r.cpu(), selfCPU()
	start := time.Now()
	var wg sync.WaitGroup
	var lag, started, finished atomic.Int64
	vctx, cancel := context.WithCancel(ctx)
	defer cancel()
	deadline := start.Add(duration + time.Duration(last)*ActionSpacing + drain)
	var timedOut atomic.Bool
	timer := time.AfterFunc(time.Until(deadline), func() { timedOut.Store(true); cancel() })
	defer timer.Stop()
	// While visits are offered, sample how many visits and requests are in flight. In a steady state
	// the numbers level off; a server that falls behind makes them grow.
	var visitsIn, requestsIn []float64
	offering := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				visitsIn = append(visitsIn, float64(started.Load()-finished.Load()))
				requestsIn = append(requestsIn, float64(r.inflight.Load()))
			case <-offering:
				return
			case <-vctx.Done():
				return
			}
		}
	}()
	for k := 0; k < n && vctx.Err() == nil; k++ {
		at := start.Add(time.Duration(float64(k) / rate * float64(time.Second)))
		if d := time.Until(at); d > 0 {
			select {
			case <-time.After(d):
			case <-vctx.Done():
			}
		}
		started.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer finished.Add(1)
			// Lateness is measured when the visit actually starts to run, not when it was spawned.
			if l := time.Since(at); l > 0 {
				for old := lag.Load(); int64(l) > old && !lag.CompareAndSwap(old, int64(l)); old = lag.Load() {
				}
			}
			r.runVisit(vctx, tr, a, seq0+k, pacer{open: true, start: at, reads: reads})
		}()
	}
	close(offering)
	<-sampled
	wg.Wait()
	end := time.Now()
	round.TimedOut, round.Backlog = timedOut.Load(), Growing(visitsIn, ramp) || Growing(requestsIn, ramp)
	round.LagMs = ms(time.Duration(lag.Load()))
	r.wl.plan(round, seq0, n, reads)
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	r.countOrders(round)
	return round
}

// Growing says whether a series sampled at equal intervals kept growing. It leaves out the first
// skip samples (the climb to a steady state: in an open model the visits in flight take as long as
// the longest visit to level off) and fits a straight line to the rest by least squares; the series is
// growing when that line rises, over the length of what was fitted, by more than 20% of the mean of
// what was fitted plus a floor of 5 units. A steady growth that is slow in each sample is caught, and a
// climb that levels off is not. With fewer than 24 samples left there is nothing to judge.
func Growing(series []float64, skip int) bool {
	if skip < 0 || skip >= len(series) || len(series)-skip < 24 {
		return false
	}
	y := series[skip:]
	m := float64(len(y))
	var sx, sy, sxx, sxy float64
	for i, v := range y {
		x := float64(i)
		sx, sy, sxx, sxy = sx+x, sy+v, sxx+x*x, sxy+x*v
	}
	slope := (m*sxy - sx*sy) / (m*sxx - sx*sx)
	return slope*m > 0.2*(sy/m)+5
}

// PlannedMix is what the planned visits of a round hold: the shopper and staff visits, the page views
// of the shopper visits, the returning visitors, and the writes by kind.
type PlannedMix struct {
	Shoppers     int `json:"shoppers"`
	Staff        int `json:"staff"`
	ShopperViews int `json:"shopperPageViews"`
	Returning    int `json:"returning"`
	Adds         int `json:"adds"`
	Checkouts    int `json:"checkouts"`
	Edits        int `json:"edits"`
	Ships        int `json:"ships"`
}

// why says how a planned mix is not the mix of the scenario, or "" when it is: the share of staff
// visits within half a point, the page views of a shopper visit within 1% of ShopperViewsMean, the
// returning visitors within half a point of ReturningShare and, unless the scenario is read-only, the
// adds and checkouts within 0.2 points of their shares and the edits and ships within 3 points of
// theirs among the staff visits. A round of whole cycles has them exactly, up to the rounding of a
// block.
func (m PlannedMix) why(sc BrowseScenario) string {
	off := func(what string, n, of int, want, within float64) string {
		got := float64(n) / float64(max(of, 1))
		if math.Abs(got-want) > within {
			return fmt.Sprintf("the planned %s are %.4f, want %.4f within %v", what, got, want, within)
		}
		return ""
	}
	if m.Shoppers == 0 {
		return "no shopper visit planned"
	}
	why := cmp.Or(
		off("staff visits a visit", m.Staff, m.Shoppers+m.Staff, sc.Staff, 0.005),
		off("page views a shopper visit", m.ShopperViews, m.Shoppers, ShopperViewsMean, 0.01*ShopperViewsMean),
		off("returning visitors a shopper visit", m.Returning, m.Shoppers, ReturningShare, 0.005),
	)
	if why != "" || sc.ReadOnly {
		return why
	}
	why = cmp.Or(
		off("adds to the cart a shopper visit", m.Adds, m.Shoppers, AddToCartShare, 0.002),
		off("checkouts a shopper visit", m.Checkouts, m.Shoppers, CheckoutShare, 0.002),
	)
	if why != "" || m.Staff == 0 {
		return why
	}
	return cmp.Or(
		off("product edits a staff visit", m.Edits, m.Staff, sc.Edit, 0.03),
		off("ships a staff visit", m.Ships, m.Staff, sc.Ship, 0.03),
	)
}
