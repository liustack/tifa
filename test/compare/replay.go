package compare

import (
	"context"
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

// BrowseScenario is one run of the browse comparison: a mix of reads and writes. Reads 95% / writes
// 5% means that of 100 user actions (a page view or a write) 5 are writes, so a write follows a page
// view with probability w/(1-w): 1/19, 1/4 and 1. These are the shares of the TPC-W mixes
// (https://www.tpc.org/tpcw/). Every write changes the customer name of an order, which the lists,
// the stats and the other orders of that customer show: the harder case for a cache.
type BrowseScenario struct {
	Name      string  `json:"name"`
	Title     string  `json:"title"`
	Mean      int     `json:"mean"` // page views of a visit, on average
	WriteRate float64 `json:"writeRate"`
	FanOut    bool    `json:"fanOut"`
}

// BrowseScenarios are the runs, in the order they are measured. A visit is 4 page views on average
// (one full load in four, 75% soft navigations), an assumption and not a measurement.
var BrowseScenarios = []BrowseScenario{
	{"r95w5", "reads 95% / writes 5%", 4, 1.0 / 19, true},
	{"r80w20", "reads 80% / writes 20%", 4, 1.0 / 4, true},
	{"r50w50", "reads 50% / writes 50%", 4, 1, true},
}

// maxConns is how many connections a browser opens to one host (HTTP/1.1): a user never has more
// requests in flight than this.
const maxConns = 6

// replayer plays traces against one server for a number of users.
type replayer struct {
	base    string
	write   Writer
	buildID string // of Tifa, put into the navigation requests it recorded
	traces  map[int]Trace
	plans   []Plan
	sc      BrowseScenario
	seed    uint64
	cpu     func() float64 // processor seconds of the server so far
	rss     func() int64   // its peak resident size in bytes
	writes  atomic.Int64   // the number of writes made, which gives each its address

	t0, t1 atomic.Int64 // the measuring window, unix nanoseconds, 0 until set
	openMu sync.Mutex
	open   map[uint64]time.Time // requests sent and not yet answered, by when they were sent
	openID uint64
	mu     sync.Mutex
	res    LoadResult
}

// LoadResult is what a step of users saw in the measuring window.
type LoadResult struct {
	Users    int     `json:"users"`
	Seconds  float64 `json:"seconds"`
	Visits   int     `json:"visits"`
	Views    int     `json:"views"`
	Errors   int     `json:"errors"`
	Requests int     `json:"requests"`
	Bytes    int64   `json:"bytes"`
	Reloads  int     `json:"reloads"`
	CPU      float64 `json:"cpuSeconds"`
	// OpenRequests are the requests still waiting for an answer when the window ended, and
	// OpenOldestMs is how long the oldest had waited. A server that hangs shows here and nowhere else.
	OpenRequests int     `json:"openRequestsAtEnd"`
	OpenOldestMs float64 `json:"openOldestMs"`
	PeakRSS      float64 `json:"peakRssMiB"`
	// ClientCPU is the processor time of the load generator itself in the window, in seconds. When it
	// is close to the cpus given to the client, the client is what limits the visits a second.
	ClientCPU float64 `json:"clientCpuSeconds"`
	N304      int     `json:"notModified"`
	// Renders counts the answers to documents by X-Tifa-Render (Tifa only).
	Renders  map[string]int `json:"renders,omitempty"`
	FirstErr string         `json:"firstError,omitempty"`
	// Responses counts the answers in the window by "<type> <status> <content type>", and the
	// answers that carry a header asking for a reload of the document.
	Responses map[string]int `json:"responses,omitempty"`
	// Times are by request type; a navigation is all the requests of one click.
	Types map[string]TypeStat `json:"types"`

	lat map[string][]float64
}

// TypeStat is the latencies of one type of request, in milliseconds.
type TypeStat struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50Ms"`
	P99 float64 `json:"p99Ms"`
}

var tifaBuild = regexp.MustCompile(`_tifa=[0-9a-f]+\.`)

func (r *replayer) inWindow(t time.Time) bool {
	a, b := r.t0.Load(), r.t1.Load()
	n := t.UnixNano()
	return a != 0 && n >= a && (b == 0 || n < b)
}

func (r *replayer) addLatency(kind string, t time.Time, ms float64) {
	if !r.inWindow(t) {
		return
	}
	r.mu.Lock()
	r.res.lat[kind] = append(r.res.lat[kind], ms)
	r.mu.Unlock()
}

func (r *replayer) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.res.FirstErr == "" {
		r.res.FirstErr = err.Error()
	}
	if r.inWindow(time.Now()) {
		r.res.Errors++
	}
}

// outcome of one request.
type outcome struct {
	start, end time.Time
	err        error
}

// visitState is what one visit keeps: cookies, the ETags of the documents it got, and the
// prefetches still running.
type visitState struct {
	client *http.Client
	etags  map[string]string
	sem    chan struct{}
	bg     sync.WaitGroup
	errMu  sync.Mutex
	err    error // the first prefetch that failed
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

func (r *replayer) send(ctx context.Context, v *visitState, q TraceReq) outcome {
	url := q.URL
	if r.buildID != "" {
		url = tifaBuild.ReplaceAllString(url, "_tifa="+r.buildID+".")
	}
	req, err := http.NewRequestWithContext(ctx, q.Method, r.base+url, nil)
	if err != nil {
		return outcome{err: err}
	}
	for k, val := range q.Headers {
		req.Header.Set(k, val)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	if q.Class != "doc" {
		// What a browser adds to a fetch of its own page. TanStack Start refuses a server function
		// call without it (403).
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
	}
	path, _, _ := strings.Cut(q.URL, "?")
	if q.INM {
		if e := v.etags[path]; e != "" {
			req.Header.Set("If-None-Match", e)
		}
	}
	v.sem <- struct{}{}
	defer func() { <-v.sem }()
	start := time.Now()
	defer r.track()()
	res, err := v.client.Do(req)
	if err != nil {
		return outcome{err: err}
	}
	n, err := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	end := time.Now()
	if err != nil {
		return outcome{err: err}
	}
	if res.StatusCode != 200 && res.StatusCode != 304 {
		return outcome{err: fmt.Errorf("%s %s: status %d", q.Method, q.URL, res.StatusCode)}
	}
	if q.Class == "doc" {
		if e := res.Header.Get("ETag"); e != "" {
			v.etags[path] = e
		}
	}
	if r.inWindow(end) {
		r.mu.Lock()
		r.res.Requests++
		r.res.Bytes += n
		if res.StatusCode == 304 {
			r.res.N304++
		}
		r.res.Responses[q.Class+" "+strconv.Itoa(res.StatusCode)+" "+mediaType(res.Header.Get("Content-Type"))]++
		if res.Header.Get("X-Remix-Reload-Document") != "" {
			r.res.Responses["reload-document header"]++
		}
		if q.Class == "doc" {
			if rd := res.Header.Get("X-Tifa-Render"); rd != "" && rd != "none" {
				r.res.Renders[rd]++
			}
		}
		r.mu.Unlock()
	}
	if q.Class == "prefetch" {
		r.addLatency("prefetch", end, float64(end.Sub(start))/float64(time.Millisecond))
	}
	return outcome{start: start, end: end}
}

// runStep plays the requests of one page view with the order and the parallelism the browser had:
// a request starts when every request that had finished before it started in the browser is done (a request that started in the same millisecond is no one's dependency, so that two quick requests cannot wait for each other).
// The page view ends when its documents and navigation requests are done. Prefetches go on in the
// background until the end of the visit.
func (r *replayer) runStep(ctx context.Context, v *visitState, s TraceStep) error {
	stepStart := time.Now() // the click or the load: a wait for a connection counts
	n := len(s.Reqs)
	done := make([]chan struct{}, n)
	for i := range done {
		done[i] = make(chan struct{})
	}
	outs := make([]outcome, n)
	var main sync.WaitGroup
	for j := range s.Reqs {
		var deps []chan struct{}
		for i := range s.Reqs {
			if i != j && s.Reqs[i].End > 0 && s.Reqs[i].End <= s.Reqs[j].Start && s.Reqs[i].Start < s.Reqs[j].Start {
				deps = append(deps, done[i])
			}
		}
		wg := &main
		if s.Reqs[j].Class == "prefetch" {
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
			outs[j] = r.send(ctx, v, s.Reqs[j])
			if s.Reqs[j].Class == "prefetch" && outs[j].err != nil && ctx.Err() == nil {
				v.fail(outs[j].err) // a prefetch that fails is a failure of the visit
			}
		}()
	}
	main.Wait()
	var last time.Time
	for j := range outs {
		if s.Reqs[j].Class == "prefetch" {
			continue // awaited at the end of the visit, and its outcome is not read here
		}
		o := outs[j]
		if o.err != nil {
			return o.err
		}
		if o.end.After(last) {
			last = o.end
		}
		if s.Reqs[j].Class == "doc" {
			kind := "doc"
			if s.Kind == StepReload {
				kind = "reload"
			}
			r.addLatency(kind, o.end, float64(o.end.Sub(stepStart))/float64(time.Millisecond))
		}
	}
	if s.Kind == StepClick && !last.IsZero() {
		r.addLatency("nav", last, float64(last.Sub(stepStart))/float64(time.Millisecond))
	}
	return nil
}

// write sends the n-th write and times it.
func (r *replayer) doWrite(ctx context.Context, v *visitState, w PlannedWrite) error {
	n := int(r.writes.Add(1))
	st := WriteStep(r.write, w.Order, n, r.sc.FanOut)
	req, err := http.NewRequestWithContext(ctx, st.Method, r.base+st.Path, strings.NewReader(string(st.Body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	start := time.Now()
	defer r.track()()
	res, err := v.client.Do(req)
	if err != nil {
		return err
	}
	b, err := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	end := time.Now()
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("write %s: status %d", st.Path, res.StatusCode)
	}
	if r.inWindow(end) {
		r.mu.Lock()
		r.res.Requests++
		r.res.Bytes += b
		r.res.Responses["write "+strconv.Itoa(res.StatusCode)+" "+mediaType(res.Header.Get("Content-Type"))]++
		r.mu.Unlock()
		r.addLatency("write", end, float64(end.Sub(start))/float64(time.Millisecond))
	}
	return nil
}

// visit plays one trace: its page views in turn, the writes of the plan after the page views the
// plan puts them after, and at the end the prefetches still running.
func (r *replayer) visit(ctx context.Context, tr *http.Transport, t Trace, plan Plan) error {
	jar, _ := cookiejar.New(nil)
	v := &visitState{client: &http.Client{Transport: tr, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, etags: map[string]string{}, sem: make(chan struct{}, maxConns)}
	writes := PlanWrites(r.seed, plan, r.sc.WriteRate)
	reloads := 0
	for i, s := range t.Steps {
		if err := r.runStep(ctx, v, s); err != nil {
			v.bg.Wait()
			return err
		}
		if s.Kind == StepReload {
			reloads++
		}
		for _, w := range writes {
			if w.After == i {
				if err := r.doWrite(ctx, v, w); err != nil {
					v.bg.Wait()
					return err
				}
			}
		}
	}
	v.bg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := v.failure(); err != nil {
		return err
	}
	if end := time.Now(); r.inWindow(end) {
		r.mu.Lock()
		r.res.Visits++
		r.res.Views += len(t.Steps)
		r.res.Reloads += reloads
		r.mu.Unlock()
	}
	return nil
}

// user plays visits one after another until ctx ends. User u takes plan (u*7919 + i) mod the number
// of plans for its i-th visit, so the sequence of a user is the same for every framework, and
// skips the plans whose trace the browser could not record.
func (r *replayer) user(ctx context.Context, u int) {
	tr := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: maxConns, MaxConnsPerHost: maxConns}
	defer tr.CloseIdleConnections()
	for i := 0; ctx.Err() == nil; i++ {
		id := (u*7919 + i) % len(r.plans)
		t, ok := r.traces[id]
		if !ok {
			continue
		}
		if err := r.visit(ctx, tr, t, r.plans[id]); err != nil && ctx.Err() == nil {
			r.fail(err)
		}
	}
}

// RunLoad plays the traces with the given number of users: warm of lead-in that is not counted,
// then one common window for all users.
func (r *replayer) RunLoad(ctx context.Context, users int, warm, window time.Duration) (*LoadResult, error) {
	r.res = LoadResult{Users: users, Renders: map[string]int{}, Responses: map[string]int{}, lat: map[string][]float64{}, Types: map[string]TypeStat{}}
	r.t0.Store(0)
	r.t1.Store(0)
	uctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for u := range users {
		wg.Add(1)
		go func() { defer wg.Done(); r.user(uctx, u) }()
	}
	if err := sleep(ctx, warm); err != nil {
		return nil, err
	}
	cpu0, client0 := r.cpu(), selfCPU()
	start := time.Now()
	r.t0.Store(start.UnixNano())
	if err := sleep(ctx, window); err != nil {
		return nil, err
	}
	end := time.Now()
	r.t1.Store(end.UnixNano())
	cpu1, client1 := r.cpu(), selfCPU()
	openN, openOldest := r.openAt(end)
	cancel()
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	res := &r.res
	res.Seconds = end.Sub(start).Seconds()
	res.CPU = cpu1 - cpu0
	res.OpenRequests, res.OpenOldestMs = openN, openOldest
	res.ClientCPU = client1 - client0
	res.PeakRSS = float64(r.rss()) / (1 << 20)
	for kind, l := range res.lat {
		sorted := sortedCopy(l)
		res.Types[kind] = TypeStat{N: len(l), P50: Percentile(sorted, 50), P99: Percentile(sorted, 99)}
	}
	return res, nil
}

// selfCPU is the processor time this process has used, in seconds.
func selfCPU() float64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
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

// track notes a request as sent and returns the function that notes its answer.
func (r *replayer) track() func() {
	r.openMu.Lock()
	defer r.openMu.Unlock()
	if r.open == nil {
		r.open = map[uint64]time.Time{}
	}
	r.openID++
	id := r.openID
	r.open[id] = time.Now()
	return func() {
		r.openMu.Lock()
		delete(r.open, id)
		r.openMu.Unlock()
	}
}

// openAt counts the requests still open at t and how long the oldest has waited, in ms.
func (r *replayer) openAt(t time.Time) (n int, oldest float64) {
	r.openMu.Lock()
	defer r.openMu.Unlock()
	for _, sent := range r.open {
		n++
		oldest = max(oldest, float64(t.Sub(sent))/float64(time.Millisecond))
	}
	return n, oldest
}
