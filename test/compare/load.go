package compare

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
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
	// Capped says that a closed round was sized to every visit the data before its clock was made for
	// (browse.prepared), or more: it plays them all and cannot be made longer (playable). It says
	// nothing of how long the round took.
	Capped bool `json:"capped,omitempty"`

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
	// SetupRequests is how many requests the setup before the clock took (the orders of the ships and
	// the carts of the returning visitors), and SetupError why the setup failed: a request of it failed
	// or rendered a page, or the render memo of Tifa held pages before the clock.
	SetupRequests int    `json:"setupRequests,omitempty"`
	SetupError    string `json:"setupError,omitempty"`
	// WarmUp is what the warm-up before the clock played, apart from the round (nil for a run with
	// none).
	WarmUp *WarmUp `json:"warmUp,omitempty"`
	// Memo is the render memo of Tifa as its metrics route counted it, before the warm-up, at the clock
	// and at the end (Tifa alone has it), and MemoError why it could not be read at the end.
	Memo      *MemoRound `json:"memo,omitempty"`
	MemoError string     `json:"memoError,omitempty"`
	// PrivateHits counts the answers of the private pages, the cart and the checkout, that came from
	// the render memo: with a cart of their own for every visitor they should be few.
	PrivateHits int `json:"privateHits"`

	// Renders counts the answers of Tifa by how it answered (X-Tifa-Render: vm, memo or shared), the
	// documents and the navigation data alike, and PageRenders splits them by kind of page.
	Renders     map[string]int            `json:"renders,omitempty"`
	PageRenders map[string]map[string]int `json:"pageRenders,omitempty"`
	// Encodings counts the answers of the page views (documents, navigation data and prefetches) by
	// their Content-Encoding, identity for none.
	Encodings map[string]int `json:"encodings,omitempty"`
	// VMWhy splits the answers Tifa rendered in a VM by kind of page and why (RenderFirst, RenderAgain,
	// RenderPrivate, RenderPrivateCart).
	VMWhy map[string]map[string]int `json:"vmWhy,omitempty"`
	// Crawler is what the crawler requests of the round did (nil for a round with none): they are no visits
	// and no page views, so no count of people has them in it. Their latency is in Types["crawler"], which
	// the rule of the round does not look at.
	Crawler *CrawlStats `json:"crawler,omitempty"`
	// Landing splits the documents of the landings of the storefront that Tifa answered by the parameter
	// they carry (LandingGroups) and how it answered them: the render memo, a render in progress that it
	// shared, or a render in a VM. A parameter is no part of the page, so the groups should be answered alike.
	Landing   map[string]map[string]int `json:"landing,omitempty"`
	Responses map[string]int            `json:"responses,omitempty"`
	Types     map[string]TypeStat       `json:"types"`
	// Series is page views a second and their P99 in steps of SeriesLength from the first request: how the
	// round went as the cache filled or the JIT warmed.
	Series []SeriesStep `json:"series,omitempty"`

	// WriteKinds counts the writes by kind. Orders is, for each project, how many orders the server held
	// at the end of a round: those of the data, the ones placed for its ships before the clock and
	// every order placed since.
	WriteKinds map[string]int `json:"writeKinds,omitempty"`
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
	r.Encodings = a.encodings
	r.VMWhy = a.vmWhy
	r.Landing = a.landing
	if r.Crawler != nil {
		r.Crawler.Done, r.Crawler.Failed, r.Crawler.Bytes, r.Crawler.OldBytes = a.crawl.done, a.crawl.failed, a.crawl.bytes, a.crawl.oldBytes
		r.Crawler.Statuses, r.Crawler.Renders = a.crawl.statuses, a.crawl.renders
	}
	r.PrivateHits = a.pageRenders[StoreKindCart][RenderHit] + a.pageRenders[StoreKindCheckout][RenderHit]
	r.WriteKinds = a.kinds
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
// the mix of the scenario, its setup before the clock (the orders of its ships, the carts of its
// returning visitors, and for Tifa a render memo that held nothing then) was made with answers that
// render nothing, the render memo of Tifa was read again at its end, and the load generator started every visit it was meant to (one that fell behind
// or ran out of cpus is found by generatorFault). Pass is about the server, on what
// it did in the round and in its warm-up: a server that fails visits or falls behind fails the rule,
// in a valid round.
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
		invalid("the setup before the clock failed: " + r.SetupError)
	}
	if r.MemoError != "" {
		invalid("the render memo could not be read after the round: " + r.MemoError)
	}
	if r.Refused > 0 {
		invalid(fmt.Sprintf("the shop refused %d requests with 409 or 422", r.Refused))
	}
	// A round that was not cut off (by its timeout, or the deadline of the open model) starts every
	// visit it planned: if not, the generator did not do its work.
	if !r.TimedOut && r.VisitsStarted < r.VisitsPlanned {
		invalid(fmt.Sprintf("the load generator started %d of the %d visits planned", r.VisitsStarted, r.VisitsPlanned))
	}
	// The warm-up is the same traffic as the round, so it is judged as the round is (below). A closed
	// warm-up cut off by its timeout ran longer than the round was sized for: a matter of sizing.
	w := r.WarmUp
	if w != nil && w.TimedOut && r.Model != "open" {
		invalid(fmt.Sprintf("the warm-up was cut off by its timeout after %.0f s, with %d of its %d visits done: the round was sized too long", w.Seconds, w.Visits, w.VisitsPlanned))
	}
	r.Pass, r.Why = true, ""
	fail := func(why string) {
		if r.Why == "" {
			r.Why = why
		}
		r.Pass = false
	}
	if w != nil {
		if w.TimedOut && r.Model == "open" {
			fail("the warm-up: the workload did not finish in time: a backlog was left")
		}
		if w.VisitsFailed > 0 || w.Failures > 0 {
			fail(fmt.Sprintf("the warm-up: %d failed visits of %d, %d failures (%s)", w.VisitsFailed, w.VisitsPlanned, w.Failures, w.FirstErr))
		}
		if !w.TimedOut && w.Writes < w.WritesPlanned {
			fail(fmt.Sprintf("the warm-up: %d of its %d writes completed", w.Writes, w.WritesPlanned))
		}
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
// Before its clock come the setup and the warm-up (warmUp), played with the same sessions. It
// starts at the first action of the round and ends when the last visit has finished, or at timeout.
func (r *runner) RunClosed(ctx context.Context, sessions, visits int, timeout time.Duration) *Round {
	round := &Round{Model: "closed", Sessions: sessions, Blocks: visits / r.wl.blockVisits()}
	ok := r.setup(ctx, round, 0, visits)
	var seen map[string]bool
	if ok {
		// The warm-up has as long for each visit as the round.
		seen, ok = r.warmUp(ctx, round, visits, func(w *Round) *acc {
			return r.playClosed(ctx, w, sessions, r.prepared, r.warm, time.Duration(float64(timeout)*max(1, float64(r.warm)/float64(max(visits, 1)))), nil)
		})
	}
	if !ok {
		r.wl.plan(round, 0, visits)
		return round
	}
	r.playClosed(ctx, round, sessions, 0, visits, timeout, seen)
	r.countOrders(round)
	r.endMemo(ctx, round)
	return round
}

// playClosed plays visits seq0 to seq0+n-1 with sessions concurrent sessions, as RunClosed says, and
// fills round with what they did. seen are the URLs Tifa answered before (the warm-up's), for the
// split of its renders by why.
func (r *runner) playClosed(ctx context.Context, round *Round, sessions, seq0, n int, timeout time.Duration, seen map[string]bool) *acc {
	a := newAcc()
	maps.Copy(a.seen, seen)
	// The slots of the stream that hold the visits seq0 to seq0+n-1 and the crawler requests among them.
	t0, slots := r.wl.humanStart(seq0), r.wl.slots(seq0, n)
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
				if s >= slots {
					return
				}
				r.playSlot(ctx, tr, a, t0+s, pacer{})
			}
		}()
	}
	wg.Wait()
	end := time.Now()
	round.TimedOut = ctx.Err() == context.DeadlineExceeded
	r.wl.plan(round, seq0, n)
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	return a
}

// countOrders notes, after a round, how many orders each project of the back office holds.
func (r *runner) countOrders(round *Round) {
	round.Orders = r.pool.orderCounts()
}

// WarmUp is what the warm-up before the clock of a round played (runner.warmUp): the visits from
// FirstVisit on, with their writes. It counts in no result of the round.
type WarmUp struct {
	FirstVisit    int            `json:"firstVisit"`
	VisitsPlanned int            `json:"visitsPlanned"`
	Visits        int            `json:"visitsSucceeded"`
	VisitsFailed  int            `json:"visitsFailed"`
	WritesPlanned int            `json:"writesPlanned"`
	Writes        int            `json:"writes"`
	WriteKinds    map[string]int `json:"writeKinds,omitempty"`
	Requests      int            `json:"requests"`
	Refused       int            `json:"refused,omitempty"`
	// Crawlers are the crawler requests of the warm-up that were answered (they are no visits, and no
	// request counted above).
	Crawlers int     `json:"crawlers,omitempty"`
	Failures int     `json:"failures"`
	FirstErr string  `json:"firstError,omitempty"`
	Seconds  float64 `json:"seconds"`
	// TimedOut says that the warm-up was cut off before every visit of it ended: a server too slow
	// for its round as well.
	TimedOut bool `json:"timedOut,omitempty"`
	// IdleCores are the cores the server used in the last second of the pause after the warm-up
	// (settlePause), where its cpu is read: what it still did with no request in flight.
	IdleCores *float64 `json:"idleCores,omitempty"`
}

// settleWait is how long the end of a warm-up waits for its requests in flight to end.
const settleWait = 30 * time.Second

// settlePause is how long every server is left alone after the last request of a warm-up has
// ended, before the metrics of Tifa are read and the clock starts. The client is done with a
// request the trace cancelled when it cancels it, while the server may still be at work on it (a
// render goes on in its VM and lands in the memo): the pause keeps that work out of the round. It is
// the same for every server.
const settlePause = 2 * time.Second

// settle waits settlePause and returns the cores the server used in its last second, or nil where
// the cpu of the server is not read.
func (r *runner) settle() *float64 {
	time.Sleep(settlePause - time.Second)
	cpu0, t0 := r.cpu(), time.Now()
	time.Sleep(time.Second)
	if !r.cpuKnown {
		return nil
	}
	cores := (r.cpu() - cpu0) / time.Since(t0).Seconds()
	return &cores
}

// warmUp plays the warm-up of a round before its clock, when the run has one (r.warm visits from
// visit r.prepared on, after the visits the data is prepared for), with play, which plays them as
// the round plays its own (with its sessions, or at its rate) and with their writes, into a round of
// their own. Then it waits until no request of it is in flight and reads the render memo of Tifa
// again (Memo.Warm), after a pause that leaves the server time to end what it still does
// (settle). It returns the URLs Tifa answered in it, and whether the round can start: a
// warm-up the shop refused a request of with 409 or 422 (after the restock no write of the plan is
// one it refuses), whose requests did not end, or after which the memo could not be read, makes the
// round invalid (SetupError), since the servers would not start the clock with the same data. The
// round is played after a warm-up that was cut off or whose visits or writes failed, and Judge
// decides it from what the warm-up did, as from the round.
func (r *runner) warmUp(ctx context.Context, round *Round, visits int, play func(w *Round) *acc) (map[string]bool, bool) {
	if r.warm == 0 {
		return nil, true
	}
	if r.prepared < visits {
		panic(fmt.Sprintf("a warm-up after %d prepared visits for a round of %d: it would play the visits of the round", r.prepared, visits))
	}
	w := &Round{}
	a := play(w)
	round.WarmUp = &WarmUp{FirstVisit: r.prepared, VisitsPlanned: w.VisitsPlanned, Visits: w.Visits, VisitsFailed: w.VisitsFailed, WritesPlanned: w.WritesPlanned, Writes: w.Writes,
		WriteKinds: w.WriteKinds, Requests: w.Requests, Crawlers: w.crawlerRequests(), Refused: w.Refused, Failures: w.Failures, FirstErr: w.FirstErr, Seconds: w.Seconds, TimedOut: w.TimedOut}
	for wait := time.Now().Add(settleWait); r.inflight.Load() > 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(wait) {
			round.SetupError = fmt.Sprintf("the warm-up left %d requests in flight %s after it ended", r.inflight.Load(), settleWait)
			return nil, false
		}
	}
	if w.Refused > 0 {
		round.SetupError = fmt.Sprintf("the shop refused %d requests of the warm-up with 409 or 422", w.Refused)
		return nil, false
	}
	round.WarmUp.IdleCores = r.settle()
	if !r.warmMemo(ctx, round) {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seen, true
}

// Size plays visits 0 to visits-1 of the scenario with their writes, on sessions concurrent sessions,
// for d or until they have all ended, and returns the visits a second: what the first round of a cell
// is sized from. Before it the setup makes the orders of their ships and the carts of their
// returning visitors, as before a round. The round it returns says why the setup failed, and counts
// the failures and the refusals of the run.
func (r *runner) Size(ctx context.Context, sessions int, d time.Duration, visits int) (float64, *Round) {
	round := &Round{}
	if !r.setup(ctx, round, 0, visits) {
		return 0, round
	}
	rate, a := r.closedFor(ctx, sessions, d, pacer{}, visits)
	round.Failures, round.Refused = a.failures, a.refused
	return rate, round
}

// closedFor plays visits on sessions concurrent sessions, each starting the next as soon as one ends,
// for d or until visits have started (no limit when visits is negative), and returns the visits a
// second that ended, completed or failed, and what it saw.
func (r *runner) closedFor(ctx context.Context, sessions int, d time.Duration, p pacer, visits int) (float64, *acc) {
	a := newAcc()
	var next, ended atomic.Int64
	limit := r.wl.humanStart(max(visits, 0)) // the slots that hold the visits asked for
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
				s := int(next.Add(1)) - 1
				if visits >= 0 && s >= limit {
					return
				}
				// Visits that failed count too (a 503 ends a visit fast): the rate sizes rounds, whose length
				// every visit sets. A crawler request is not a visit, and counts none.
				if r.playSlot(wctx, tr, a, s, p) {
					ended.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	return float64(ended.Load()) / time.Since(start).Seconds(), a
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
// sequence from seq0. Before its clock come the setup and the warm-up (warmUp), played at the same
// rate, with warmDrain.
func (r *runner) RunOpen(ctx context.Context, rate float64, n int, drain time.Duration, seq0 int) *Round {
	round := &Round{Model: "open", Rate: rate}
	ok := r.setup(ctx, round, seq0, n)
	var seen map[string]bool
	if ok {
		seen, ok = r.warmUp(ctx, round, seq0+n, func(w *Round) *acc {
			return r.playOpen(ctx, w, rate, r.warm, warmDrain, r.prepared, nil)
		})
	}
	if !ok {
		r.wl.plan(round, seq0, n)
		return round
	}
	r.playOpen(ctx, round, rate, n, drain, seq0, seen)
	r.countOrders(round)
	r.endMemo(ctx, round)
	return round
}

// warmDrain is how long after its last planned action the warm-up of an open round may run: its
// visits end, or are cut off, before the clock of the round. Tests shorten it.
var warmDrain = 20 * time.Second

// playOpen plays visits seq0 to seq0+n-1 at rate a second, as RunOpen says, and fills round with
// what they did. seen are the URLs Tifa answered before (the warm-up's), for the split of its
// renders by why.
func (r *runner) playOpen(ctx context.Context, round *Round, rate float64, n int, drain time.Duration, seq0 int, seen map[string]bool) *acc {
	a := newAcc()
	maps.Copy(a.seen, seen)
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
	var lag atomic.Int64
	var flying flight
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
				visitsIn = append(visitsIn, float64(flying.visits()))
				requestsIn = append(requestsIn, float64(r.inflight.Load()))
			case <-offering:
				return
			case <-vctx.Done():
				return
			}
		}
	}()
	// The slots of the stream are offered evenly over the time the visits take at their rate, so people arrive
	// at rate visits a second and the crawler requests arrive among them in the proportion of the stream.
	t0, slots := r.wl.humanStart(seq0), r.wl.slots(seq0, n)
	for k := 0; k < slots && vctx.Err() == nil; k++ {
		at := start.Add(time.Duration(float64(k) / rate * float64(n) / float64(slots) * float64(time.Second)))
		if d := time.Until(at); d > 0 {
			select {
			case <-time.After(d):
			case <-vctx.Done():
			}
		}
		crawler, _ := r.wl.slot(t0 + k)
		flying.start(crawler)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer flying.end(crawler)
			// Lateness is measured when the visit actually starts to run, not when it was spawned.
			if l := time.Since(at); l > 0 {
				for old := lag.Load(); int64(l) > old && !lag.CompareAndSwap(old, int64(l)); old = lag.Load() {
				}
			}
			r.playSlot(vctx, tr, a, t0+k, pacer{open: true, start: at})
		}()
	}
	close(offering)
	<-sampled
	wg.Wait()
	end := time.Now()
	round.TimedOut, round.Backlog = timedOut.Load(), Growing(visitsIn, ramp) || Growing(requestsIn, ramp)
	round.LagMs = ms(time.Duration(lag.Load()))
	r.wl.plan(round, seq0, n)
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	return a
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
	ClickIDs     int `json:"clickIds"`
	EmailParams  int `json:"emailParams"`
	// Crawlers are the crawler requests among the slots of the planned visits.
	Crawlers  int `json:"crawlers"`
	Adds      int `json:"adds"`
	Checkouts int `json:"checkouts"`
	Edits     int `json:"edits"`
	Ships     int `json:"ships"`
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
		off("landings with a click id a shopper visit", m.ClickIDs, m.Shoppers, ClickIDShare, 0.005),
		off("landings with an email parameter a shopper visit", m.EmailParams, m.Shoppers, EmailParamShare, 0.005),
		m.crawlerShare(sc),
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

// crawlerShare is the check of the crawler requests of a scenario that has them: among the HTML
// requests of the planned slots (the full load of every visit, and the crawler requests) they are within
// half a point of the share of the scenario.
func (m PlannedMix) crawlerShare(sc BrowseScenario) string {
	if sc.Crawler <= 0 {
		return ""
	}
	got := float64(m.Crawlers) / float64(max(m.Shoppers+m.Staff+m.Crawlers, 1))
	if math.Abs(got-sc.Crawler) > 0.005 {
		return fmt.Sprintf("the planned crawler requests among the HTML requests are %.4f, want %.4f within 0.005", got, sc.Crawler)
	}
	return ""
}

// crawlerRequests is how many crawler requests of the round were answered, with the status expected or not.
func (r *Round) crawlerRequests() int {
	if r.Crawler == nil {
		return 0
	}
	return r.Crawler.Done + r.Crawler.Failed
}

// flight counts what is in flight in the open model: the visits of people and the crawler requests
// apart. A backlog is the visits of people that keep piling up, so the count that is judged for it
// (Growing) has people alone: crawler requests in flight, steady or not, would raise the mean the
// growth is measured against and hide a backlog of people, or show a change of their own as one.
type flight struct{ people, crawlers atomic.Int64 }

func (f *flight) start(crawler bool) { f.count(crawler).Add(1) }
func (f *flight) end(crawler bool)   { f.count(crawler).Add(-1) }
func (f *flight) count(crawler bool) *atomic.Int64 {
	if crawler {
		return &f.crawlers
	}
	return &f.people
}

// visits is the visits of people in flight.
func (f *flight) visits() int64 { return f.people.Load() }
