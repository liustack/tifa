package compare

import (
	"context"
	"fmt"
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
	Framework string  `json:"framework"`
	Mix       string  `json:"mix"`
	Model     string  `json:"model"`                     // "closed" or "open"
	Users     int     `json:"users,omitempty"`           // closed
	Rate      float64 `json:"visitsPerSecond,omitempty"` // open: visits started a second
	Round     int     `json:"round"`
	Blocks    int     `json:"blocks"`

	Visits    int            `json:"visits"` // visits that completed
	PageViews int            `json:"pageViews"`
	FullLoads int            `json:"fullLoads"`
	Clicks    int            `json:"clicks"`
	Writes    int            `json:"writes"`
	Pages     map[string]int `json:"pagesByClass"`

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
	// Lag is the largest time a visit started after it was scheduled (open model): the generator
	// itself falling behind.
	LagMs float64 `json:"maxStartLagMs,omitempty"`
	// Backlog says that the visits or the requests in flight kept growing while visits were being
	// offered (open model): the server was not keeping up, even if it caught up after the end.
	Backlog bool `json:"backlogGrowing,omitempty"`
	// GeneratorLimited says that the load generator, not the server, ended the round: it started
	// visits late or used most of its cpus.
	GeneratorLimited bool `json:"generatorLimited,omitempty"`

	Renders   map[string]int      `json:"renders,omitempty"`
	Responses map[string]int      `json:"responses,omitempty"`
	Types     map[string]TypeStat `json:"types"`

	// Valid says that the round achieved the mix it is named for: write share within one point, the
	// share of clicks within one point and 4 page views a visit within 1%. A round that is not valid
	// is run again.
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
	r.Pages = a.pages
	r.Failures, r.FirstErr = a.failures, a.firstErr
	r.Requests, r.Cancelled, r.Bytes, r.N304 = a.requests, a.cancelled, a.bytes, a.n304
	r.Renders, r.Responses = a.renders, a.responses
	r.Seconds = end.Sub(start).Seconds()
	if !a.lastAction.IsZero() && a.lastAction.After(start) {
		r.DrainSeconds = end.Sub(a.lastAction).Seconds()
	}
	r.CPUSeconds, r.ClientCPU = cpu1-cpu0, client1-client0
	r.PeakRSS = float64(rss) / (1 << 20)
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

// Judge sets Valid and Pass of a round. The mix is checked on what the round achieved, and the rule
// on every class of action.
func (r *Round) Judge(mix Mix) {
	r.Valid, r.WhyInvalid = true, ""
	switch {
	case r.Visits == 0:
		r.Valid, r.WhyInvalid = false, "no visit completed"
	case math.Abs(r.WriteShare-mix.Share) > 0.01:
		r.Valid, r.WhyInvalid = false, fmt.Sprintf("write share %.3f, want %.2f within 0.01", r.WriteShare, mix.Share)
	case math.Abs(r.ClickShare-0.75) > 0.01:
		r.Valid, r.WhyInvalid = false, fmt.Sprintf("click share %.3f, want 0.75 within 0.01", r.ClickShare)
	case math.Abs(r.ViewsPerVisit-4) > 0.04:
		r.Valid, r.WhyInvalid = false, fmt.Sprintf("%.3f page views a visit, want 4 within 1%%", r.ViewsPerVisit)
	}
	r.Pass, r.Why = true, ""
	fail := func(why string) {
		if r.Why == "" {
			r.Why = why
		}
		r.Pass = false
	}
	if r.TimedOut {
		fail("the workload did not finish in time: a backlog was left")
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

// RunClosed plays visits seq 0..visits-1 of the global sequence with users users, each taking the
// next visit when it finishes one (zero think time), and each visit with at most six connections.
// It starts at the first scheduled action and ends when the last visit has finished, or at timeout.
func (r *runner) RunClosed(ctx context.Context, users, visits int, timeout time.Duration) *Round {
	a := newAcc()
	var next atomic.Int64
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cpu0, client0 := r.cpu(), selfCPU()
	start := time.Now()
	var wg sync.WaitGroup
	for range users {
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
	round := &Round{Model: "closed", Users: users, Blocks: visits / BlockVisits, TimedOut: ctx.Err() == context.DeadlineExceeded}
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	return round
}

// Warm plays the workload with users users for d, counting nothing, and returns how many visits a
// second it completed. It is the warm-up of a framework, apart from the measured round, and it sizes
// that round.
func (r *runner) Warm(ctx context.Context, users int, d time.Duration) float64 {
	a := newAcc()
	var next atomic.Int64
	wctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	for range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: maxConns, MaxConnsPerHost: maxConns, IdleConnTimeout: idleConn}
			defer tr.CloseIdleConnections()
			for wctx.Err() == nil {
				r.runVisit(wctx, tr, a, int(next.Add(1))-1, pacer{})
			}
		}()
	}
	wg.Wait()
	a.mu.Lock()
	defer a.mu.Unlock()
	return float64(a.visits) / time.Since(start).Seconds()
}

// BlocksFor sizes a workload: the whole blocks (a multiple of the number of blocks of the plans,
// at least that many) that a framework completing rate visits a second plays in about target.
func BlocksFor(rate float64, target time.Duration, planBlocks int) int {
	blocks := int(math.Ceil(rate * target.Seconds() / BlockVisits / float64(planBlocks)))
	return max(blocks, 1) * planBlocks
}

// RunOpen starts visits at rate a second, on schedule whether or not the earlier ones are done,
// n visits in all (a whole number of blocks, so that the mix comes out exact). The actions of a visit are planned ActionSpacing apart, and latency is counted from
// the planned time. It ends when the last visit has finished, or drain after the last planned
// action. The visits are the global sequence from seq0.
func (r *runner) RunOpen(ctx context.Context, rate float64, n int, drain time.Duration, seq0 int) *Round {
	a := newAcc()
	duration := time.Duration(float64(n) / rate * float64(time.Second))
	tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: 4096, MaxConnsPerHost: 0, IdleConnTimeout: idleConn}
	defer tr.CloseIdleConnections()
	last := 0 // the longest plan, in actions, for the drain deadline
	for i := range min(n, len(r.wl.Plans)) {
		p := r.wl.planFor(seq0 + i)
		acts := len(p.Steps)
		for j := range p.Steps {
			if _, ok := r.wl.Writes[[2]int{p.ID, j}]; ok {
				acts++
			}
		}
		last = max(last, acts)
	}
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
			r.runVisit(vctx, tr, a, seq0+k, pacer{open: true, start: at})
		}()
	}
	close(offering)
	<-sampled
	wg.Wait()
	end := time.Now()
	round := &Round{Model: "open", Rate: rate, TimedOut: timedOut.Load(), Backlog: Growing(visitsIn) || Growing(requestsIn)}
	round.LagMs = ms(time.Duration(lag.Load()))
	a.round(round, cpu0, r.cpu(), client0, selfCPU(), start, end, r.rss())
	return round
}

// Growing says whether a series sampled at equal intervals kept growing. It leaves out the first
// 35% (the climb to a steady state) and fits a straight line to the rest by least squares; the series
// is growing when that line rises, over the length of what was fitted, by more than 20% of the mean of
// what was fitted plus a floor of 5 units. A steady growth that is slow in each sample is caught, and a
// climb that levels off is not.
func Growing(series []float64) bool {
	n := len(series)
	if n < 8 {
		return false
	}
	y := series[n*35/100:]
	m := float64(len(y))
	var sx, sy, sxx, sxy float64
	for i, v := range y {
		x := float64(i)
		sx, sy, sxx, sxy = sx+x, sy+v, sxx+x*x, sxy+x*v
	}
	slope := (m*sxy - sx*sy) / (m*sxx - sx*sx)
	return slope*m > 0.2*(sy/m)+5
}
