package compare

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// BrowseScenario is one workload of the browse comparison: a site, a share of the user actions (a
// page view or a write) that are writes, and which writes they are. The shares are those of the
// TPC-W mixes (https://www.tpc.org/tpcw/): browsing 5%, shopping 20% and ordering 50% of the actions
// are writes. Which writes they are is the Mix: "office" (place, ship, annotate and fix the delivery
// of orders, MixOffice), "rename" (every write gives an order a new customer name, which the lists,
// the stats and the other orders of that customer show: the harder case for a cache, MixRename) or
// "store" (a visitor of the storefront fills a cart and checks out, MixStore).
type BrowseScenario struct {
	Name  string  `json:"name"`
	Title string  `json:"title"`
	Share float64 `json:"writeShare"`
	Mix   string  `json:"mix"`
}

// site is the site the scenario replays visits to: the storefront for the store mix, else the
// workbench.
func (s BrowseScenario) site() string {
	if s.Mix == MixStore {
		return SiteStore
	}
	return SiteWorkbench
}

// BrowseScenarios are the scenarios, in the order they are measured. A visit is 4 page views on
// average (one full load in four, 75% client navigations), an assumption and not a measurement.
var BrowseScenarios = []BrowseScenario{
	{"office-w5", "back office, reads 95% / writes 5%", 0.05, MixOffice},
	{"office-w20", "back office, reads 80% / writes 20%", 0.20, MixOffice},
	{"office-w50", "back office, reads 50% / writes 50%", 0.50, MixOffice},
	{"r95w5", "reads 95% / writes 5%, every write renames a customer", 0.05, MixRename},
	{"r80w20", "reads 80% / writes 20%, every write renames a customer", 0.20, MixRename},
	{"r50w50", "reads 50% / writes 50%, every write renames a customer", 0.50, MixRename},
	{"store-w5", "storefront, reads 95% / writes 5%", 0.05, MixStore},
	{"store-w20", "storefront, reads 80% / writes 20%", 0.20, MixStore},
	{"store-w50", "storefront, reads 50% / writes 50%", 0.50, MixStore},
}

// DefaultScenarios are the scenarios that run when none is asked for: the back office at the three
// rates, the rename stress at 20% only and the storefront at 20% only.
const DefaultScenarios = "office-w5,office-w20,office-w50,r80w20,store-w20"

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
// the writes that the scenario puts after some page views. A run plays visits in the order of a
// global sequence: visit s is plan s mod len(Plans), so a whole block is played before the next.
type Workload struct {
	Plans    []Plan
	Traces   map[int]Trace // by plan ID
	Writes   map[[2]int]PlannedWrite
	Scenario BrowseScenario
}

// planFor is the plan that visit seq of the global sequence plays: the plans in order, over and over.
func (w *Workload) planFor(seq int) Plan { return w.Plans[seq%len(w.Plans)] }

// NewWorkload puts the writes of a scenario into the plans, block by block, and fails if a plan has
// no trace.
func NewWorkload(set *PlanSet, traces map[int]Trace, sc BrowseScenario, seed uint64) (*Workload, error) {
	w := &Workload{Plans: set.Plans, Traces: traces, Writes: map[[2]int]PlannedWrite{}, Scenario: sc}
	for _, p := range set.Plans {
		if _, ok := traces[p.ID]; !ok {
			return nil, fmt.Errorf("plan %d has no trace", p.ID)
		}
	}
	for b := range set.Blocks {
		ws, err := BlockWrites(seed, set.Plans[b*BlockVisits:(b+1)*BlockVisits], sc)
		if err != nil {
			return nil, err
		}
		for _, x := range ws {
			w.Writes[[2]int{x.Plan, x.After}] = x
		}
	}
	return w, nil
}

// runner plays a workload against one server.
type runner struct {
	base       string
	write      Writer
	create     func(project string) string // the path that places an order in a project
	storeWrite func(path string) string    // the path in this app of a write of the storefront
	buildID    string                      // of Tifa, put into the navigation requests it recorded
	wl         *Workload
	seed       uint64
	// inflight counts the requests sent and not yet answered, for the check that no backlog grows.
	inflight atomic.Int64
	cpu      func() float64 // processor seconds of the server so far
	rss      func() int64   // its peak resident size in bytes
	writes   atomic.Int64   // the number of writes made, which gives each rename its address
	pool     pool           // the orders that office writes go to, kept as long as the server lives
}

// mix is the mix of writes the runner plays.
func (r *runner) mix() string { return r.wl.Scenario.Mix }

var tifaBuild = regexp.MustCompile(`_tifa=[0-9a-f]+\.`)

// acc collects what a run saw, as it happens: a page view when it completes, a request when it
// completes, a write when it is answered.
type acc struct {
	mu                                  sync.Mutex
	visits, views, loads, clicks, wrote int
	failures, requests, cancelled, n304 int
	bytes                               int64
	firstErr                            string
	pages                               map[string]int
	renders, responses                  map[string]int
	lat                                 map[string][]float64
	lastAction                          time.Time
	// kinds counts the writes by kind, and poolEmpty the office writes that found no order to go to
	// and were sent as a create instead (they are among the creates).
	kinds     map[string]int
	poolEmpty int
}

func newAcc() *acc {
	return &acc{pages: map[string]int{}, renders: map[string]int{}, responses: map[string]int{}, lat: map[string][]float64{}, kinds: map[string]int{}}
}

func (a *acc) fail(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	if a.firstErr == "" {
		a.firstErr = err.Error()
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// visitState is what one visit keeps: cookies, the ETags it got, the connections it may use and its
// background work.
type visitState struct {
	client *http.Client
	sem    chan struct{}
	etagMu sync.Mutex
	etags  map[string]string // by URL
	bg     sync.WaitGroup
	errMu  sync.Mutex
	err    error // the first background request that failed
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
// read. cancelAt is that time (zero for a request the browser let finish).
func (r *runner) send(ctx context.Context, v *visitState, a *acc, q TraceReq, cancelAt time.Time) outcome {
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
	var body io.Reader
	if q.Body != "" {
		body = strings.NewReader(q.Body)
	}
	req, err := http.NewRequestWithContext(rctx, q.Method, r.base+url, body)
	if err != nil {
		return outcome{end: time.Now(), err: err}
	}
	for k, val := range q.Headers {
		req.Header.Set(k, val)
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
	start := time.Now()
	sent = true
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	res, err := v.client.Do(req)
	if err != nil {
		return cancelled(err)
	}
	responded = true
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
	if err != nil {
		return cancelled(err)
	}
	if e := res.Header.Get("ETag"); e != "" && res.StatusCode == 200 {
		v.setETag(q.URL, e)
	}
	a.mu.Lock()
	a.requests++
	a.bytes += n
	if res.StatusCode == 304 {
		a.n304++
	}
	a.responses[q.Class+" "+strconv.Itoa(res.StatusCode)+" "+mediaType(res.Header.Get("Content-Type"))]++
	if rd := res.Header.Get("X-Remix-Reload-Document"); rd != "" {
		a.responses["reload-document header"]++
	}
	if q.Class == "doc" {
		if rd := res.Header.Get("X-Tifa-Render"); rd != "" && rd != "none" {
			a.renders[rd]++
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
// latency is counted from it. It returns the time of the end of the page view, and whether it was
// successful: no foreground request failed and at least one finished.
func (r *runner) runStep(ctx context.Context, v *visitState, a *acc, s TraceStep, scheduled time.Time) (time.Time, error) {
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
			outs[j] = r.send(ctx, v, a, q, cancelAt)
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
//
// A warm-up plays its visits with reads set: it sends no writes, so that the data a measured run
// starts with is the same for every framework, whatever the warm-up did.
type pacer struct {
	open  bool
	start time.Time
	reads bool
}

func (p pacer) at(j int) time.Time {
	if p.open {
		return p.start.Add(time.Duration(j) * ActionSpacing)
	}
	return time.Now()
}

// runVisit plays plan number seq of the global sequence: its page views in turn, the writes of the
// scenario after the page views the plan puts them after, and at the end the background requests
// the browser still had. It reports whether the visit completed.
func (r *runner) runVisit(ctx context.Context, tr *http.Transport, a *acc, seq int, p pacer) bool {
	plan := r.wl.planFor(seq)
	trace := r.wl.Traces[plan.ID]
	jar, _ := cookiejar.New(nil)
	v := &visitState{client: &http.Client{Transport: tr, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sem: make(chan struct{}, maxConns)}
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
	for i, s := range trace.Steps {
		sched := wait()
		if ctx.Err() != nil {
			return false
		}
		if _, err := r.runStep(ctx, v, a, s, sched); err != nil {
			if ctx.Err() == nil {
				a.fail(err)
			}
			v.bg.Wait()
			return false
		}
		if w, ok := r.wl.Writes[[2]int{plan.ID, i}]; ok && !p.reads {
			sched := wait()
			if err := r.doWrite(ctx, v, a, w, sched); err != nil {
				if ctx.Err() == nil {
					a.fail(err)
				}
				v.bg.Wait()
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
