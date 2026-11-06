package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

// BrowseFrameworks are the specs the browse comparison runs: each framework as shipped.
var BrowseFrameworks = []string{"tifa", "nextjs", "start", "reactrouter"}

// BrowseOptions say how to run the browse comparison. The traces are the files `compare record`
// wrote, and the plans they were made from.
type BrowseOptions struct {
	TraceDir     string
	OutJSON      string // written after every round, and read to continue a run that stopped
	Seed         uint64
	Servers      []string
	ServerCPUs   string
	LoadCPUs     string
	LoadCPUCount int      // how many cpus the load may use, to tell a saturated generator
	Mixes        []Mix    // all of them when empty
	Phases       []string // "closed", "sustain" and "latency"; all of them when empty
	Users        int      // the users of the throughput table, 32
	ExtraUsers   []int    // more users, for the first mix only: 8 and 16
	Rounds       int
	Warm         time.Duration // the warm-up of a closed round, apart from it
	Target       time.Duration // how long a closed round should take, about
	TrialWindow  time.Duration // an open trial of the search for the sustainable rate
	FinalWindow  time.Duration // an open round of the latency table, at least
	Stamp        string        // what the traces and the builds belong to, kept in the result
	Log          io.Writer
}

// BrowseResult is everything the browse comparison measured, round by round.
type BrowseResult struct {
	Stamp   string            `json:"stamp"`
	Date    string            `json:"date"`
	Seed    uint64            `json:"seed"`
	Machine Machine           `json:"machine"`
	Place   Placement         `json:"placement"`
	Version map[string]string `json:"versions"`
	Config  map[string]string `json:"settings"`
	Plans   struct {
		Blocks int `json:"blocks"`
		PlanCounts
	} `json:"plans"`
	Mixes []Mix `json:"mixes"`
	// Closed holds every round at a fixed number of users, the invalid ones that were run again too.
	Closed []Round `json:"closed"`
	// Blocks is the size of the measured workload of each framework, mix and users (whole blocks),
	// taken from the warm-up of its first round.
	Blocks map[string]int `json:"blocks"`
	// Sustain is, per framework, the search for the arrival rate it sustains in the open model.
	Sustain map[string]*Sustain `json:"sustain"`
	// LatencyRate is the rate that all four were run at: half of the lowest sustainable rate.
	LatencyRate float64 `json:"latencyRate"`
	LatencyBase string  `json:"latencyBase"`
	Latency     []Round `json:"latency"`
	// Checks come from the answers of one round of each framework (the first of the first mix).
	Checks map[string]*TraceCheck `json:"traceCheck,omitempty"`
}

// Sustain is the search for the arrival rate, in visits a second, that one framework sustains: the
// pass rule for every class of action over three rounds. Rate is 0 until the search is done.
type Sustain struct {
	Trials  []Round `json:"trials"`
	Confirm []Round `json:"confirm"`
	Rate    float64 `json:"rate"`
	// Limited says that the load generator, not the server, ended the search.
	Limited bool   `json:"generatorLimited,omitempty"`
	Done    bool   `json:"done"`
	Note    string `json:"note,omitempty"`
}

type browse struct {
	e     Env
	o     BrowseOptions
	res   *BrowseResult
	specs map[string]Spec
	set   *PlanSet
	trace map[string]map[int]Trace
	bid   string
}

func (b *browse) logf(format string, a ...any) { fmt.Fprintf(b.o.Log, format, a...) }

func (b *browse) save() error {
	if b.o.OutJSON == "" {
		return nil
	}
	data, err := json.Marshal(b.res)
	if err != nil {
		return err
	}
	return os.WriteFile(b.o.OutJSON+".tmp", data, 0o644)
}

func (b *browse) commit() error {
	if err := b.save(); err != nil || b.o.OutJSON == "" {
		return err
	}
	return os.Rename(b.o.OutJSON+".tmp", b.o.OutJSON)
}

// LoadTraces reads the plans and the traces of the specs and fails unless every spec has a trace
// for every plan: a replay on an incomplete set would not play the same visits on every server.
func LoadTraces(dir string, specs []string) (*PlanSet, map[string]map[int]Trace, error) {
	set, err := ReadPlans(dir)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]map[int]Trace{}
	for _, name := range specs {
		traces, dropped, err := ReadTraces(TracePath(dir, name))
		if err != nil {
			return nil, nil, err
		}
		m := map[int]Trace{}
		for _, t := range traces {
			m[t.Plan] = t
		}
		if dropped > 0 || len(m) != len(set.Plans) {
			return nil, nil, fmt.Errorf("%s holds %d of the %d plans (%d visits failed to record): record again", TracePath(dir, name), len(m), len(set.Plans), dropped)
		}
		for _, p := range set.Plans {
			t, ok := m[p.ID]
			if !ok {
				return nil, nil, fmt.Errorf("%s has no trace for plan %d", TracePath(dir, name), p.ID)
			}
			if len(t.Steps) != len(p.Steps) {
				return nil, nil, fmt.Errorf("%s: plan %d has %d page views, its trace %d", TracePath(dir, name), p.ID, len(p.Steps), len(t.Steps))
			}
		}
		out[name] = m
	}
	return set, out, nil
}

// Browse runs the browse comparison and returns what it measured. Prepare must have run.
func (e Env) Browse(ctx context.Context, o BrowseOptions) (*BrowseResult, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if len(o.Servers) == 0 {
		o.Servers = BrowseFrameworks
	}
	if len(o.Mixes) == 0 {
		o.Mixes = Mixes
	}
	if len(o.Phases) == 0 {
		o.Phases = []string{"closed", "sustain", "latency"}
	}
	if o.Users == 0 {
		o.Users = 32
	}
	if o.Rounds == 0 {
		o.Rounds = 3
	}
	placement, err := PlacementFor(runtime.GOOS, SysTopology(), o.ServerCPUs, o.LoadCPUs)
	if err != nil {
		return nil, err
	}
	if o.LoadCPUs != "" {
		if err := pinSelf(o.LoadCPUs); err != nil {
			return nil, err
		}
	}
	b := &browse{e: e, o: o, specs: map[string]Spec{}}
	var specs []Spec
	for _, name := range o.Servers {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no spec named %q", name)
		}
		b.specs[name] = e.Specs()[i]
		specs = append(specs, e.Specs()[i])
	}
	if b.set, b.trace, err = LoadTraces(o.TraceDir, o.Servers); err != nil {
		return nil, err
	}
	if b.bid, err = e.tifaBuildID(); err != nil {
		return nil, err
	}
	b.res = &BrowseResult{}
	if data, err := os.ReadFile(o.OutJSON); err == nil && o.OutJSON != "" {
		if err := json.Unmarshal(data, b.res); err != nil {
			return nil, fmt.Errorf("%s: %w", o.OutJSON, err)
		}
		if b.res.Stamp != o.Stamp {
			return nil, fmt.Errorf("%s belongs to other sources, plans or settings than this run:\n  file: %s\n  here: %s\nremove it to start again", o.OutJSON, b.res.Stamp, o.Stamp)
		}
		b.logf("continuing %s: %d closed rounds, %d latency rounds done\n", o.OutJSON, len(b.res.Closed), len(b.res.Latency))
	} else {
		b.res.Stamp, b.res.Date, b.res.Seed = o.Stamp, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), o.Seed
		b.res.Machine, b.res.Place, b.res.Version = machine(), placement, e.versions(specs)
		b.res.Mixes = o.Mixes
		b.res.Plans.Blocks = b.set.Blocks
		for _, p := range b.set.Plans {
			b.res.Plans.PlanCounts.Add(p, 0)
		}
		b.res.Config = map[string]string{
			"users": fmt.Sprint(o.Users), "extraUsers": fmt.Sprint(o.ExtraUsers), "rounds": fmt.Sprint(o.Rounds), "warm": o.Warm.String(), "target": o.Target.String(),
			"trialWindow": o.TrialWindow.String(), "finalWindow": o.FinalWindow.String(), "serverCpus": o.ServerCPUs, "loadCpus": o.LoadCPUs,
		}
		b.res.Blocks = map[string]int{}
		b.res.Sustain = map[string]*Sustain{}
	}
	b.logf("placement: %s\n", placement.Note)
	has := func(p string) bool { return slices.Contains(o.Phases, p) }
	if has("closed") {
		if err := b.closed(ctx); err != nil {
			return b.res, err
		}
	}
	if has("sustain") {
		if err := b.sustain(ctx); err != nil {
			return b.res, err
		}
	}
	if has("latency") {
		if err := b.latency(ctx); err != nil {
			return b.res, err
		}
	}
	return b.res, b.commit()
}

// with starts a fresh server of a framework, checks that a write shows in its pages, and calls f
// with a runner for the workload of a mix. The server is stopped after.
func (b *browse) with(ctx context.Context, name string, mix Mix, f func(*runner) error) error {
	if err := waitIdle(ctx, b.o.Log, b.o.ServerCPUs+","+b.o.LoadCPUs); err != nil {
		return err
	}
	spec := b.specs[name]
	server, err := spec.Launch(b.o.ServerCPUs)
	if err != nil {
		return err
	}
	defer server.Stop()
	if err := server.WaitListening(ctx); err != nil {
		return err
	}
	wl, err := NewWorkload(b.set, b.trace[name], mix, b.o.Seed)
	if err != nil {
		return err
	}
	r := &runner{base: server.Base, write: spec.Write, wl: wl, cpu: server.CPUSeconds, rss: server.PeakRSS}
	if spec.Framework == "tifa" {
		r.buildID = b.bid
	}
	if err := r.verifyWrites(ctx); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return f(r)
}

func (b *browse) usersFor(mix Mix) []int {
	users := []int{b.o.Users}
	if mix.Name == Mixes[0].Name {
		users = append(slices.Clone(b.o.ExtraUsers), users...)
	}
	return users
}

func (b *browse) closedDone(fw, mix string, users, round int) bool {
	return slices.ContainsFunc(b.res.Closed, func(r Round) bool {
		return r.Framework == fw && r.Mix == mix && r.Users == users && r.Round == round && r.Valid
	})
}

// closed is the throughput table: every framework at the same number of users, the same mix, three
// rounds in a rotating order with fresh servers.
func (b *browse) closed(ctx context.Context) error {
	for _, mix := range b.o.Mixes {
		for _, users := range b.usersFor(mix) {
			failed := map[string]bool{}
			for round := 1; round <= b.o.Rounds; round++ {
				for _, fw := range Rotate(b.o.Servers, round-1) {
					if b.closedDone(fw, mix.Name, users, round) {
						continue
					}
					if failed[fw] && round > 1 && b.o.Rounds > 1 {
						// A hung or timed-out round is reported as failed; the other rounds of that cell are skipped.
						if last := b.lastClosed(fw, mix.Name, users); last != nil && last.TimedOut {
							continue
						}
					}
					r, err := b.closedRound(ctx, fw, mix, users, round)
					if err != nil {
						return err
					}
					if !r.Pass {
						failed[fw] = true
					}
				}
			}
		}
	}
	return nil
}

func (b *browse) lastClosed(fw, mix string, users int) *Round {
	for i := len(b.res.Closed) - 1; i >= 0; i-- {
		r := &b.res.Closed[i]
		if r.Framework == fw && r.Mix == mix && r.Users == users {
			return r
		}
	}
	return nil
}

func (b *browse) closedRound(ctx context.Context, fw string, mix Mix, users, round int) (*Round, error) {
	key := fmt.Sprintf("%s/%d/%s", mix.Name, users, fw)
	for attempt := 1; ; attempt++ {
		var out *Round
		err := b.with(ctx, fw, mix, func(r *runner) error {
			rate := r.Warm(ctx, users, b.o.Warm)
			blocks, ok := b.res.Blocks[key]
			if !ok {
				blocks = BlocksFor(rate, b.o.Target, b.set.Blocks)
				b.res.Blocks[key] = blocks
			}
			out = r.RunClosed(ctx, users, blocks*BlockVisits, 4*b.o.Target+time.Minute)
			return nil
		})
		if err != nil {
			return nil, err
		}
		out.Framework, out.Mix, out.Round, out.Attempt = fw, mix.Name, round, attempt
		out.Judge(mix)
		b.res.Closed = append(b.res.Closed, *out)
		if err := b.commit(); err != nil {
			return nil, err
		}
		b.logf("closed %-9s %2d users %-12s round %d (try %d): %d blocks, %.1f page views/s, %.2f ms CPU/page view, writes %.1f%%, %d failures, pass %v %s valid %v %s\n",
			mix.Name, users, fw, round, attempt, out.Blocks, float64(out.PageViews)/out.Seconds, 1000*out.CPUSeconds/math.Max(1, float64(out.PageViews)), 100*out.WriteShare, out.Failures, out.Pass, out.Why, out.Valid, out.WhyInvalid)
		if out.Valid || attempt >= 3 {
			return out, nil
		}
	}
}

// visitsFor is the whole blocks of visits that last about window at rate visits a second.
func visitsFor(rate float64, window time.Duration) int {
	return max(1, int(math.Ceil(rate*window.Seconds()/BlockVisits))) * BlockVisits
}

// openRound runs the open model on a fresh server: a warm-up at the same rate, apart, then n visits.
func (b *browse) openRound(ctx context.Context, fw string, mix Mix, rate float64, window time.Duration, warm time.Duration) (*Round, error) {
	var out *Round
	err := b.with(ctx, fw, mix, func(r *runner) error {
		r.RunOpen(ctx, rate, max(1, int(rate*warm.Seconds())), 20*time.Second, 0) // discarded
		n := visitsFor(rate, window)
		out = r.RunOpen(ctx, rate, n, 30*time.Second, 0)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Framework, out.Mix = fw, mix.Name
	out.Judge(mix)
	// The load generator must keep up: visits start on time and it is not out of processor time.
	if out.LagMs > 250 && out.Pass {
		out.Pass, out.Why = false, fmt.Sprintf("the load generator started a visit %.0f ms late", out.LagMs)
	}
	if b.o.LoadCPUCount > 0 && out.Seconds > 0 && out.ClientCPU/out.Seconds > 0.8*float64(b.o.LoadCPUCount) && out.Pass {
		out.Pass, out.Why = false, fmt.Sprintf("the load generator used %.2f cores of %d", out.ClientCPU/out.Seconds, b.o.LoadCPUCount)
	}
	return out, nil
}

func generatorWhy(r *Round) bool {
	return strings.HasPrefix(r.Why, "the load generator")
}

// sustain finds, for each framework, the arrival rate it sustains in the open model, on the
// first mix: double the rate while the rule holds, narrow it to within 10%, then require the rule
// over three rounds, stepping down 10% until it holds.
func (b *browse) sustain(ctx context.Context) error {
	mix := b.o.Mixes[0]
	for _, fw := range b.o.Servers {
		s := b.res.Sustain[fw]
		if s != nil && s.Done {
			continue
		}
		s = &Sustain{}
		b.res.Sustain[fw] = s
		trial := func(rate float64) (bool, error) {
			r, err := b.openRound(ctx, fw, mix, rate, b.o.TrialWindow, b.o.TrialWindow/3)
			if err != nil {
				return false, err
			}
			s.Trials = append(s.Trials, *r)
			b.logf("sustain %-12s %8.2f visits/s: pass %v %s (%d visits, load P99 %.0f, click P99 %.0f ms, client %.2f cores)\n", fw, rate, r.Pass, r.Why, r.Visits, r.Types["load"].P99, r.Types["click"].P99, r.ClientCPU/math.Max(r.Seconds, 1))
			if !r.Pass && generatorWhy(r) {
				s.Limited = true
			}
			return r.Pass, b.commit()
		}
		// Double from 4 visits a second while the rule holds. If 4 fails, halve until one holds.
		lo, hi, rate := 0.0, 0.0, 4.0
		for rate <= 16384 {
			ok, err := trial(rate)
			if err != nil {
				return err
			}
			if !ok {
				hi = rate
				if lo == 0 && rate > 0.5 {
					rate /= 2
					continue
				}
				break
			}
			if hi > 0 {
				lo = rate // found going down: the rate that passes, with hi the one above that failed
				break
			}
			lo, rate = rate, rate*2
		}
		if lo == 0 {
			s.Note, s.Done = "it does not sustain even half a visit a second", true
			continue
		}
		for hi > 0 && hi-lo > 0.1*lo {
			mid := math.Round((lo+hi)/2*100) / 100
			ok, err := trial(mid)
			if err != nil {
				return err
			}
			if ok {
				lo = mid
			} else {
				hi = mid
			}
		}
		rate = lo
		for try := 0; try < 4; try++ {
			s.Confirm = nil
			pass := true
			for round := 1; round <= b.o.Rounds; round++ {
				r, err := b.openRound(ctx, fw, mix, rate, b.o.TrialWindow*2, b.o.TrialWindow/3)
				if err != nil {
					return err
				}
				r.Round = round
				s.Confirm = append(s.Confirm, *r)
				b.logf("confirm %-12s %8.2f visits/s round %d: pass %v %s\n", fw, rate, round, r.Pass, r.Why)
				if err := b.commit(); err != nil {
					return err
				}
				if !r.Pass {
					pass = false
					break
				}
			}
			if pass {
				break
			}
			rate = math.Round(rate*0.9*100) / 100
		}
		s.Rate, s.Done = rate, true
		if hi == 0 {
			s.Note = "the search reached its cap without failing"
		}
		if err := b.commit(); err != nil {
			return err
		}
	}
	return nil
}

// latency is the latency table: all four at the same arrival rate, half of the lowest rate any of
// them sustains, on the first mix, three rounds in a rotating order with fresh servers.
func (b *browse) latency(ctx context.Context) error {
	mix := b.o.Mixes[0]
	if b.res.LatencyRate == 0 {
		low, base := 0.0, ""
		for _, fw := range b.o.Servers {
			s := b.res.Sustain[fw]
			if s == nil || !s.Done || s.Rate == 0 {
				return fmt.Errorf("no sustainable rate for %s: run the sustain phase first", fw)
			}
			if low == 0 || s.Rate < low {
				low, base = s.Rate, fw
			}
		}
		b.res.LatencyRate, b.res.LatencyBase = math.Round(low/2*100)/100, base
		if err := b.commit(); err != nil {
			return err
		}
	}
	rate := b.res.LatencyRate
	b.logf("latency rate %.2f visits/s: half of %s\n", rate, b.res.LatencyBase)
	// Long enough for at least 1100 full loads and clicks, and for at least the final window.
	window := max(b.o.FinalWindow, time.Duration(1100/rate*float64(time.Second)))
	for round := 1; round <= b.o.Rounds; round++ {
		for _, fw := range Rotate(b.o.Servers, round-1) {
			if slices.ContainsFunc(b.res.Latency, func(r Round) bool { return r.Framework == fw && r.Round == round }) {
				continue
			}
			r, err := b.openRound(ctx, fw, mix, rate, window, b.o.TrialWindow/3)
			if err != nil {
				return err
			}
			r.Round = round
			b.res.Latency = append(b.res.Latency, *r)
			b.logf("latency %-12s round %d: %d visits, load P50 %.1f P99 %.1f, click P50 %.1f P99 %.1f ms, pass %v %s\n", fw, round, r.Visits, r.Types["load"].P50, r.Types["load"].P99, r.Types["click"].P50, r.Types["click"].P99, r.Pass, r.Why)
			if err := b.commit(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e Env) tifaBuildID() (string, error) {
	data, err := os.ReadFile(e.Shop + "/.tifa/dist/build.json")
	if err != nil {
		return "", fmt.Errorf("build the shop first: %w", err)
	}
	var built struct {
		BuildID string `json:"buildId"`
	}
	if err := json.Unmarshal(data, &built); err != nil {
		return "", err
	}
	return built.BuildID, nil
}

// verifyWrites makes a write to each writable order and checks that the document of the order shows
// it and so does the data a client navigation fetches for it (as the browser asked for it in a
// trace).
func (r *runner) verifyWrites(ctx context.Context) error {
	client := &http.Client{}
	get := func(url string, headers map[string]string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", r.base+url, nil)
		if err != nil {
			return "", err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept-Encoding", "identity")
		if headers != nil {
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		res, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			return "", fmt.Errorf("GET %s: %d %v", url, res.StatusCode, err)
		}
		return string(body), nil
	}
	for _, w := range WritableOrders {
		n := int(r.writes.Add(1))
		st := WriteStep(r.write, w, n, true)
		if err := Send(ctx, r.base, st); err != nil {
			return err
		}
		digits := fmt.Sprintf("%08d", n)
		url := "/projects/" + w.Project + "/orders/" + w.Order
		doc, err := get(url, nil)
		if err != nil {
			return err
		}
		if !strings.Contains(doc, digits) || !strings.Contains(doc, st.Write.Customer) {
			return fmt.Errorf("the document of %s does not show write %d", w.Order, n)
		}
		q, ok := r.navRequest(url)
		if !ok {
			continue
		}
		if r.buildID != "" {
			q.URL = tifaBuild.ReplaceAllString(q.URL, "_tifa="+r.buildID+".")
		}
		body, err := get(q.URL, q.Headers)
		if err != nil {
			return err
		}
		if !strings.Contains(body, digits) {
			return fmt.Errorf("the navigation data of %s (%s) does not show write %d", w.Order, q.URL, n)
		}
	}
	return nil
}

// navRequest finds, in the traces, the data request of a click to a page.
func (r *runner) navRequest(page string) (TraceReq, bool) {
	for _, p := range r.wl.Plans {
		for _, s := range r.wl.Traces[p.ID].Steps {
			if s.Kind != StepClick || s.Page != page {
				continue
			}
			for _, q := range s.Reqs {
				if q.Class == "nav" && q.Cancel == "" && q.Method == "GET" && !strings.Contains(q.URL, "__manifest") {
					return q, true
				}
			}
		}
	}
	return TraceReq{}, false
}
