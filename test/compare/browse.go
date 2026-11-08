package compare

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
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
	LoadCPUCount int              // how many cpus the load may use, to tell a saturated generator
	Scenarios    []BrowseScenario // the default ones when empty
	Phases       []string         // "closed", "sustain" and "latency"; all of them when empty
	Users        int              // the users of the throughput table, 32
	ExtraUsers   []int            // more users, for the scenarios with 5% writes only: 8 and 16
	Rounds       int
	Warm         time.Duration // the warm-up of a closed round, apart from it
	Target       time.Duration // how long a closed round should take, about
	TrialWindow  time.Duration // an open trial of the search for the sustainable rate
	FinalWindow  time.Duration // an open round of the latency table, at least
	MaxRate      float64       // the search for the sustainable rate stops here, visits a second (16384 when 0)
	Stamp        string        // what the traces and the builds belong to, kept in the result
	Log          io.Writer
}

// PlanInfo is what the plans of a site hold.
type PlanInfo struct {
	Blocks int `json:"blocks"`
	PlanCounts
}

// BrowseResult is everything the browse comparison measured, round by round.
type BrowseResult struct {
	Stamp     string              `json:"stamp"`
	Date      string              `json:"date"`
	Seed      uint64              `json:"seed"`
	Machine   Machine             `json:"machine"`
	Place     Placement           `json:"placement"`
	Version   map[string]string   `json:"versions"`
	Config    map[string]string   `json:"settings"`
	Plans     map[string]PlanInfo `json:"plans"` // by site: "workbench" or "store"
	Scenarios []BrowseScenario    `json:"scenarios"`
	// Servers is, per scenario, the specs it runs, in the order of the tables.
	Servers map[string][]string `json:"servers"`
	// Closed holds every round at a fixed number of users, the invalid ones that were run again too.
	Closed []Round `json:"closed"`
	// Blocks is the size of the measured workload of each scenario, number of users and spec (whole
	// blocks), taken from the warm-up of its first round.
	Blocks map[string]int `json:"blocks"`
	// Sustain is, per scenario and spec ("office-w5/tifa"), the search for the arrival rate it
	// sustains in the open model.
	Sustain map[string]*Sustain `json:"sustain"`
	// LatencyRate is, per scenario, the rate that all its servers were run at: half of the lowest
	// sustainable rate, which LatencyBase had.
	LatencyRate map[string]float64 `json:"latencyRate"`
	LatencyBase map[string]string  `json:"latencyBase"`
	Latency     []Round            `json:"latency"`
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

// siteName is how a site is named in the result.
func siteName(site string) string {
	if site == SiteWorkbench {
		return "workbench"
	}
	return site
}

type browse struct {
	e     Env
	o     BrowseOptions
	res   *BrowseResult
	specs map[string]Spec
	// servers are the spec ids of each scenario.
	servers map[string][]string
	// sets and traces are by site, and the traces by the name a spec replays (traceName).
	sets  map[string]*PlanSet
	trace map[string]map[string]map[int]Trace
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

// LoadTraces reads the plans of a site and the traces of the names given, and fails unless every
// name has a trace for every plan: a replay on an incomplete set would not play the same visits on
// every server.
func LoadTraces(dir, site string, names []string) (*PlanSet, map[string]map[int]Trace, error) {
	set, err := ReadPlans(dir, site)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]map[int]Trace{}
	for _, name := range names {
		path := TracePath(dir, name, site)
		traces, dropped, err := ReadTraces(path)
		if err != nil {
			return nil, nil, err
		}
		m := map[int]Trace{}
		for _, t := range traces {
			m[t.Plan] = t
		}
		if dropped > 0 || len(m) != len(set.Plans) {
			return nil, nil, fmt.Errorf("%s holds %d of the %d plans (%d visits failed to record): record again", path, len(m), len(set.Plans), dropped)
		}
		for _, p := range set.Plans {
			t, ok := m[p.ID]
			if !ok {
				return nil, nil, fmt.Errorf("%s has no trace for plan %d", path, p.ID)
			}
			if len(t.Steps) != len(p.Steps) {
				return nil, nil, fmt.Errorf("%s: plan %d has %d page views, its trace %d", path, p.ID, len(p.Steps), len(t.Steps))
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
	if len(o.Scenarios) == 0 {
		for _, name := range strings.Split(DefaultScenarios, ",") {
			sc, _ := BrowseScenarioByName(name)
			o.Scenarios = append(o.Scenarios, sc)
		}
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
	raiseFileLimit()
	placement, err := PlacementFor(runtime.GOOS, SysTopology(), o.ServerCPUs, o.LoadCPUs)
	if err != nil {
		return nil, err
	}
	if o.LoadCPUs != "" {
		if err := pinSelf(o.LoadCPUs); err != nil {
			return nil, err
		}
	}
	b := &browse{e: e, o: o, specs: map[string]Spec{}, servers: map[string][]string{}, sets: map[string]*PlanSet{}, trace: map[string]map[string]map[int]Trace{}}
	var specs []Spec
	names := map[string][]string{} // by site: the traces it needs
	for _, sc := range o.Scenarios {
		list, err := e.browseSpecs(sc, o.Servers)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			b.servers[sc.Name] = append(b.servers[sc.Name], s.Name)
			if _, ok := b.specs[s.Name]; !ok {
				b.specs[s.Name] = s
				specs = append(specs, s)
			}
			if !slices.Contains(names[sc.site()], traceName(s)) {
				names[sc.site()] = append(names[sc.site()], traceName(s))
			}
		}
	}
	for site, list := range names {
		if b.sets[site], b.trace[site], err = LoadTraces(o.TraceDir, site, list); err != nil {
			return nil, err
		}
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
		b.res.Scenarios, b.res.Servers = o.Scenarios, b.servers
		b.res.Plans = map[string]PlanInfo{}
		for site, set := range b.sets {
			info := PlanInfo{Blocks: set.Blocks}
			for _, p := range set.Plans {
				info.PlanCounts.Add(p, 0)
			}
			b.res.Plans[siteName(site)] = info
		}
		b.res.Config = map[string]string{
			"users": fmt.Sprint(o.Users), "extraUsers": fmt.Sprint(o.ExtraUsers), "rounds": fmt.Sprint(o.Rounds), "warm": o.Warm.String(), "target": o.Target.String(),
			"trialWindow": o.TrialWindow.String(), "finalWindow": o.FinalWindow.String(), "serverCpus": o.ServerCPUs, "loadCpus": o.LoadCPUs,
		}
		b.res.Blocks = map[string]int{}
		b.res.Sustain = map[string]*Sustain{}
		b.res.LatencyRate, b.res.LatencyBase = map[string]float64{}, map[string]string{}
	}
	b.logf("placement: %s\n", placement.Note)
	has := func(p string) bool { return slices.Contains(o.Phases, p) }
	for _, phase := range []struct {
		name string
		run  func(context.Context, BrowseScenario) error
	}{{"closed", b.closed}, {"sustain", b.sustain}, {"latency", b.latency}} {
		if !has(phase.name) {
			continue
		}
		for _, sc := range o.Scenarios {
			if err := phase.run(ctx, sc); err != nil {
				return b.res, err
			}
		}
	}
	return b.res, b.commit()
}

// with starts a fresh server of a spec, checks that the writes of the scenario show in its pages,
// and calls f with a runner for the workload of the scenario. The round f returns is judged, and
// when it passed the writes are checked again, on the server it ran against. The server is stopped
// after. Every round starts a fresh server, so the data is the same at the start of every round:
// the orders placed in one round are not there in the next.
func (b *browse) with(ctx context.Context, name string, sc BrowseScenario, f func(*runner) *Round) (*Round, error) {
	if err := waitIdle(ctx, b.o.Log, b.o.ServerCPUs+","+b.o.LoadCPUs); err != nil {
		return nil, err
	}
	spec := b.specs[name]
	server, err := spec.Launch(b.o.ServerCPUs)
	if err != nil {
		return nil, err
	}
	defer server.Stop()
	if err := server.WaitListening(ctx); err != nil {
		return nil, err
	}
	site := sc.site()
	wl, err := NewWorkload(b.sets[site], b.trace[site][traceName(spec)], sc, b.o.Seed)
	if err != nil {
		return nil, err
	}
	r := &runner{base: server.Base, write: spec.Write, create: spec.Create, storeWrite: spec.StoreWrite, wl: wl, seed: b.o.Seed, cpu: server.CPUSeconds, rss: server.PeakRSS}
	if spec.Framework == "tifa" {
		r.buildID = b.bid
	}
	if err := r.verifyWrites(ctx); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", sc.Name, name, err)
	}
	out := f(r)
	out.Framework, out.Scenario = name, sc.Name
	out.Judge(sc)
	if out.Pass {
		if err := r.verifyWrites(ctx); err != nil {
			return nil, fmt.Errorf("%s/%s, after the round: %w", sc.Name, name, err)
		}
	}
	return out, nil
}

// usersFor are the numbers of users of the closed rounds of a scenario: o.Users, and o.ExtraUsers
// too for a scenario with the open run (5% writes).
func (b *browse) usersFor(sc BrowseScenario) []int {
	users := []int{b.o.Users}
	if sc.open() {
		users = append(slices.Clone(b.o.ExtraUsers), users...)
	}
	return users
}

func (b *browse) closedDone(fw, sc string, users, round int) bool {
	return slices.ContainsFunc(b.res.Closed, func(r Round) bool {
		return r.Framework == fw && r.Scenario == sc && r.Users == users && r.Round == round && r.Valid
	})
}

// closed is the throughput table of a scenario: every server at the same number of users, three
// rounds in a rotating order with fresh servers.
func (b *browse) closed(ctx context.Context, sc BrowseScenario) error {
	for _, users := range b.usersFor(sc) {
		failed := map[string]bool{}
		for round := 1; round <= b.o.Rounds; round++ {
			for _, fw := range Rotate(b.servers[sc.Name], round-1) {
				if b.closedDone(fw, sc.Name, users, round) {
					continue
				}
				if failed[fw] && round > 1 && b.o.Rounds > 1 {
					// A hung or timed-out round is reported as failed; the other rounds of that cell are skipped.
					if last := b.lastClosed(fw, sc.Name, users); last != nil && last.TimedOut {
						continue
					}
				}
				r, err := b.closedRound(ctx, fw, sc, users, round)
				if err != nil {
					return err
				}
				if !r.Pass {
					failed[fw] = true
				}
			}
		}
	}
	return nil
}

func (b *browse) lastClosed(fw, sc string, users int) *Round {
	for i := len(b.res.Closed) - 1; i >= 0; i-- {
		r := &b.res.Closed[i]
		if r.Framework == fw && r.Scenario == sc && r.Users == users {
			return r
		}
	}
	return nil
}

func (b *browse) closedRound(ctx context.Context, fw string, sc BrowseScenario, users, round int) (*Round, error) {
	key := fmt.Sprintf("%s/%d/%s", sc.Name, users, fw)
	planBlocks := b.sets[sc.site()].Blocks
	for attempt := 1; ; attempt++ {
		out, err := b.with(ctx, fw, sc, func(r *runner) *Round {
			rate := r.Warm(ctx, users, b.o.Warm)
			blocks, ok := b.res.Blocks[key]
			if !ok {
				blocks = BlocksFor(rate, b.o.Target, planBlocks)
				b.res.Blocks[key] = blocks
			}
			// The warm-up sends no writes, so a server whose writes make it slower (a cache that they
			// clear) does more in it than in the round, and the first round of a cell can be sized too
			// large. It is stopped at twice the target, which is too long to be valid, and resized from
			// what it did.
			return r.RunClosed(ctx, users, blocks*BlockVisits, 2*b.o.Target)
		})
		if err != nil {
			return nil, err
		}
		out.Round, out.Attempt = round, attempt
		// A round takes 0.8 to 1.6 times the target (60 to 120 seconds by default). Outside that it is
		// invalid: the workload is resized from what this round did, and the round is run again.
		if lo, hi := roundLength(b.o.Target); out.Valid && out.Visits > 0 && (out.Seconds < lo || out.Seconds > hi) {
			out.Valid, out.WhyInvalid = false, fmt.Sprintf("the round took %.0f s, outside %.0f to %.0f s: resized", out.Seconds, lo, hi)
			b.res.Blocks[key] = BlocksFor(float64(out.Visits)/out.Seconds, b.o.Target, planBlocks)
		}
		b.res.Closed = append(b.res.Closed, *out)
		if err := b.commit(); err != nil {
			return nil, err
		}
		b.logf("closed %-10s %2d users %-12s round %d (try %d): %d blocks, %.1f page views/s, %.2f ms CPU/page view, writes %.1f%%, %d failures, pass %v %s valid %v %s\n",
			sc.Name, users, fw, round, attempt, out.Blocks, float64(out.PageViews)/out.Seconds, 1000*out.CPUSeconds/math.Max(1, float64(out.PageViews)), 100*out.WriteShare, out.Failures, out.Pass, out.Why, out.Valid, out.WhyInvalid)
		if out.Valid || attempt >= 3 {
			return out, nil
		}
	}
}

// roundLength is the shortest and the longest a closed round may take, in seconds, for a target.
func roundLength(target time.Duration) (lo, hi float64) {
	return 0.8 * target.Seconds(), 1.6 * target.Seconds()
}

// visitsFor is the number of visits that last about window at rate visits a second, rounded up to
// whole cycles of all the plans, so that the page mix and the writes come out as the mix says: a
// part of a cycle has another number of page views a visit and another share of writes.
func visitsFor(rate float64, window time.Duration, cycle int) int {
	n := int(math.Ceil(rate * window.Seconds()))
	return max(1, int(math.Ceil(float64(n)/float64(cycle)))) * cycle
}

// openRound runs the open model on a fresh server: a warm-up at the same rate with no writes, apart,
// then whole cycles of all the plans.
func (b *browse) openRound(ctx context.Context, fw string, sc BrowseScenario, rate float64, window time.Duration, warm time.Duration) (*Round, error) {
	cycle := len(b.sets[sc.site()].Plans)
	out, err := b.with(ctx, fw, sc, func(r *runner) *Round {
		r.RunOpen(ctx, rate, max(1, int(rate*warm.Seconds())), 20*time.Second, 0, true) // discarded
		return r.RunOpen(ctx, rate, visitsFor(rate, window, cycle), 30*time.Second, 0, false)
	})
	if err != nil {
		return nil, err
	}
	// The load generator is checked first, so that a generator that cannot keep up is never reported
	// as a limit of the server: visits must start on time and it must have processor time to spare.
	why := ""
	if out.LagMs > 250 {
		why = fmt.Sprintf("the load generator started a visit %.0f ms late", out.LagMs)
	}
	if b.o.LoadCPUCount > 0 && out.Seconds > 0 && out.ClientCPU/out.Seconds > 0.8*float64(b.o.LoadCPUCount) {
		why = fmt.Sprintf("the load generator used %.2f cores of %d", out.ClientCPU/out.Seconds, b.o.LoadCPUCount)
	}
	if why != "" {
		out.GeneratorLimited, out.Pass, out.Why = true, false, why
	}
	return out, nil
}

// sustainKey names the search of one spec in one scenario.
func sustainKey(sc, spec string) string { return sc + "/" + spec }

// sustain finds the arrival rate that Next.js sustains in the open model on a scenario with the open
// run: double the rate while the rule holds, narrow it to within 10%, then require the rule over the
// rounds, stepping down 10% until it holds. Only Next.js is searched: half of its rate is the rate of
// the open run for every server.
func (b *browse) sustain(ctx context.Context, sc BrowseScenario) error {
	if !sc.open() {
		return nil
	}
	if !slices.Contains(b.servers[sc.Name], openBase) {
		return fmt.Errorf("%s: the open run is set by %s, which is not among the servers", sc.Name, BrowseLabel(openBase))
	}
	for _, fw := range []string{openBase} {
		key := sustainKey(sc.Name, fw)
		s := b.res.Sustain[key]
		if s != nil && s.Done {
			continue
		}
		s = &Sustain{}
		b.res.Sustain[key] = s
		trial := func(rate float64) (bool, error) {
			r, err := b.openRound(ctx, fw, sc, rate, b.o.TrialWindow, b.o.TrialWindow/3)
			if err != nil {
				return false, err
			}
			s.Trials = append(s.Trials, *r)
			b.logf("sustain %-10s %-12s %8.2f visits/s: pass %v %s (%d visits, load P99 %.0f, click P99 %.0f ms, client %.2f cores)\n", sc.Name, fw, rate, r.Pass, r.Why, r.Visits, r.Types["load"].P99, r.Types["click"].P99, r.ClientCPU/math.Max(r.Seconds, 1))
			if r.GeneratorLimited {
				s.Limited = true
			}
			return r.Pass, b.commit()
		}
		// Double from 4 visits a second while the rule holds. If 4 fails, halve until one holds.
		lo, hi, rate, maxRate := 0.0, 0.0, 4.0, cmp.Or(b.o.MaxRate, 16384)
		for rate <= maxRate {
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
			if err := b.commit(); err != nil {
				return err
			}
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
		// Only a rate that passed all its confirmation rounds is published. Every round stays in the record.
		confirmed, rounds, err := ConfirmRate(lo, 4, b.o.Rounds, func(rate float64, round int) (*Round, error) {
			r, err := b.openRound(ctx, fw, sc, rate, b.o.TrialWindow*2, b.o.TrialWindow/3)
			if err != nil {
				return nil, err
			}
			b.logf("confirm %-10s %-12s %8.2f visits/s round %d: pass %v %s\n", sc.Name, fw, rate, round, r.Pass, r.Why)
			s.Confirm = append(s.Confirm, *r)
			return r, b.commit()
		}, maxRate)
		if err != nil {
			return err
		}
		s.Confirm = rounds
		s.Rate, s.Done = confirmed, true
		if confirmed == 0 {
			s.Note = fmt.Sprintf("no sustainable rate in range: no rate from %.2f down in four steps of 10%% passed %d rounds", lo, b.o.Rounds)
		}
		if hi == 0 && confirmed > 0 {
			s.Note = "the search reached its cap without failing"
		}
		if err := b.commit(); err != nil {
			return err
		}
	}
	return nil
}

// openRate is the rate of the open run of a scenario: half of the rate Next.js sustains on it.
func openRate(res *BrowseResult, sc BrowseScenario) (float64, error) {
	s := res.Sustain[sustainKey(sc.Name, openBase)]
	if s == nil || !s.Done {
		return 0, fmt.Errorf("%s: no sustainable rate of %s: run the sustain phase first", sc.Name, BrowseLabel(openBase))
	}
	if s.Rate == 0 {
		return 0, fmt.Errorf("%s: %s has no sustainable rate in range (%s), so the open run has no rate", sc.Name, BrowseLabel(openBase), s.Note)
	}
	return math.Round(s.Rate/2*100) / 100, nil
}

// latency is the open run of a scenario: all its servers at the same arrival rate, half of the rate
// Next.js sustains on that scenario, the rounds in a rotating order with fresh servers. A server that
// cannot hold the rate fails at it, and its rounds are kept as they are: the rate is never lowered
// for it.
func (b *browse) latency(ctx context.Context, sc BrowseScenario) error {
	if !sc.open() {
		return nil
	}
	if b.res.LatencyRate[sc.Name] == 0 {
		rate, err := openRate(b.res, sc)
		if err != nil {
			return err
		}
		b.res.LatencyRate[sc.Name], b.res.LatencyBase[sc.Name] = rate, openBase
		if err := b.commit(); err != nil {
			return err
		}
	}
	rate := b.res.LatencyRate[sc.Name]
	b.logf("latency %s: %.2f visits/s, half of %s\n", sc.Name, rate, b.res.LatencyBase[sc.Name])
	// Long enough for at least 1100 full loads and clicks, and for at least the final window.
	window := max(b.o.FinalWindow, time.Duration(1100/rate*float64(time.Second)))
	for round := 1; round <= b.o.Rounds; round++ {
		for _, fw := range Rotate(b.servers[sc.Name], round-1) {
			if slices.ContainsFunc(b.res.Latency, func(r Round) bool { return r.Framework == fw && r.Scenario == sc.Name && r.Round == round }) {
				continue
			}
			r, err := b.openRound(ctx, fw, sc, rate, window, b.o.TrialWindow/3)
			if err != nil {
				return err
			}
			r.Round = round
			b.res.Latency = append(b.res.Latency, *r)
			b.logf("latency %-10s %-12s round %d: %d visits, load P50 %.1f P99 %.1f, click P50 %.1f P99 %.1f ms, pass %v %s\n", sc.Name, fw, round, r.Visits, r.Types["load"].P50, r.Types["load"].P99, r.Types["click"].P50, r.Types["click"].P99, r.Pass, r.Why)
			if err := b.commit(); err != nil {
				return err
			}
		}
	}
	return nil
}

// storeTifas are the Tifa servers the storefront scenarios run in place of "tifa": the shop with
// only the whole-page cache off (the render cache stays on) and the shop with both caches off.
// The storefront pages have the whole-page cache, which no other framework has, so the comparison
// shows Tifa with it off, and with the render cache on and off.
var storeTifas = []string{"tifa-nopage", "tifa-nocache"}

// browseServers are the spec ids a scenario runs for the servers asked for. The storefront runs the
// two Tifa servers of storeTifas where "tifa" is asked for.
func browseServers(sc BrowseScenario, names []string) []string {
	if sc.site() != SiteStore {
		return names
	}
	var out []string
	for _, name := range names {
		if name == "tifa" {
			for _, t := range storeTifas {
				if !slices.Contains(names, t) {
					out = append(out, t)
				}
			}
			continue
		}
		out = append(out, name)
	}
	return out
}

// browseSpecs are the specs a scenario runs.
func (e Env) browseSpecs(sc BrowseScenario, names []string) ([]Spec, error) {
	var specs []Spec
	for _, name := range browseServers(sc, names) {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no spec named %q", name)
		}
		specs = append(specs, e.Specs()[i])
	}
	return specs, nil
}

func (e Env) tifaBuildID() (string, error) {
	data, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "dist", "build.json"))
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

// verifyWrites checks, before and after a round, that what the writes of the scenario change is
// what the server shows next: in the document of a page and in the data a client navigation fetches
// for it (as the browser asked for it in a trace). It uses addresses and ids of its own, so the
// server is not what it was, but alike on every server.
func (r *runner) verifyWrites(ctx context.Context) error {
	switch r.mix() {
	case MixStore:
		return r.verifyStore(ctx)
	case MixRename:
		return r.verifyRename(ctx)
	}
	return r.verifyOffice(ctx)
}

// get reads a page or the navigation data of a page.
func (r *runner) get(ctx context.Context, url string, headers map[string]string) (string, error) {
	return r.getAs(ctx, http.DefaultClient, url, headers)
}

// getAs reads it with a client of its own, for a page that depends on cookies.
func (r *runner) getAs(ctx context.Context, c *http.Client, url string, headers map[string]string) (string, error) {
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
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 {
		return "", fmt.Errorf("GET %s: %d %v", url, res.StatusCode, err)
	}
	return string(b), nil
}

// nav reads the data a client navigation fetches for a page, as a trace recorded the browser asking
// for it. ok is false when no trace has a click to that page.
func (r *runner) nav(ctx context.Context, page string) (data string, ok bool, err error) {
	return r.navAs(ctx, http.DefaultClient, page)
}

// navAs is nav with a client of its own.
func (r *runner) navAs(ctx context.Context, c *http.Client, page string) (data string, ok bool, err error) {
	q, ok := r.navRequest(page)
	if !ok {
		return "", false, nil
	}
	if r.buildID != "" {
		q.URL = tifaBuild.ReplaceAllString(q.URL, "_tifa="+r.buildID+".")
	}
	data, err = r.getAs(ctx, c, q.URL, q.Headers)
	return data, true, err
}

// shows checks that the document of a page, and the navigation data of it when a trace has them,
// hold every text.
func (r *runner) shows(ctx context.Context, what, page string, texts ...string) error {
	doc, err := r.get(ctx, page, nil)
	if err != nil {
		return err
	}
	for _, s := range texts {
		if !strings.Contains(doc, s) {
			return fmt.Errorf("%s: the document of %s does not show %q", what, page, s)
		}
	}
	data, ok, err := r.nav(ctx, page)
	if err != nil || !ok {
		return err
	}
	for _, s := range texts {
		if !strings.Contains(data, s) {
			return fmt.Errorf("%s: the navigation data of %s does not show %q", what, page, s)
		}
	}
	return nil
}

// verifyOffice makes each write of the office mix once and checks what it changes.
//
//  1. Place an order with a note of its own: its page, the list of the project and the data of the
//     list show it.
//  2. Add a note to SH-1040, which has shipped (a note is allowed after shipping).
//  3. Change the recipient of an order that has not shipped.
//  4. Ship the order of step 1: its page and the list show the tracking number, and SH-1039, an
//     order that shipped long ago, shows something else than before, since it lists the carriers
//     with their parcels.
//
// The data of an order that was placed here has no trace to ask a navigation request from, so it is
// read as a document. The order of step 1 is shipped in step 4, so the pool of the writes is what it
// was.
func (r *runner) verifyOffice(ctx context.Context) error {
	v := &visitState{client: &http.Client{}}
	call := func(method, path string, body any) ([]byte, error) {
		a, _, _, err := r.request(ctx, v, method, path, mustJSON(body))
		if err == nil && a.status != 200 {
			err = fmt.Errorf("%s %s: status %d: %.200s", method, path, a.status, a.body)
		}
		return a.body, err
	}
	n := int(r.writes.Add(1))
	digits := fmt.Sprintf("%08d", n)

	note := "核对订单 " + digits
	placed, err := call(http.MethodPost, r.create("studio"), map[string]any{
		"customer": "林夏", "phone": "13800138000", "email": "linxia@example.com", "address": "上海市徐汇区 · 演示地址", "note": note,
		"items": []map[string]any{{"slug": "pour-over-kettle", "quantity": 1}, {"slug": "ceramic-cup", "quantity": 2}},
	})
	if err != nil {
		return err
	}
	var order struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(placed, &order); err != nil || order.ID == "" {
		return fmt.Errorf("the order placed has no id: %v %.200s", err, placed)
	}
	r.pool.countCreated(r.seed, "studio")
	if err := r.shows(ctx, "place an order", "/projects/studio/orders/"+order.ID, note); err != nil {
		return err
	}
	if err := r.shows(ctx, "place an order", "/projects/studio", order.ID); err != nil {
		return err
	}

	text := "核对备注 " + digits
	if _, err := call(http.MethodPut, r.write("studio", "SH-1040"), map[string]any{"text": text}); err != nil {
		return err
	}
	if err := r.shows(ctx, "add a note", "/projects/studio/orders/SH-1040", text); err != nil {
		return err
	}

	// The recipient of an order that waits: SH-1041 when the pool still holds it (before any write of a
	// round, and its page has a trace), else the order placed above.
	target, customer, phone := order.ID, "林夏", "13800138000"
	if r.pool.holds(r.seed, "studio", "SH-1041") {
		target, customer, phone = "SH-1041", WritableOrders[1].Customer, WritableOrders[1].Phone
	}
	if _, err := call(http.MethodPatch, r.write("studio", target), map[string]any{"customer": customer, "phone": phone, "address": Address(n)}); err != nil {
		return err
	}
	if err := r.shows(ctx, "change the recipient", "/projects/studio/orders/"+target, digits); err != nil {
		return err
	}

	other := "/projects/studio/orders/SH-1039"
	beforeDoc, err := r.get(ctx, other, nil)
	if err != nil {
		return err
	}
	beforeData, haveData, err := r.nav(ctx, other)
	if err != nil {
		return err
	}
	tracking := "VF" + digits
	if _, err := call(http.MethodPost, r.write("studio", order.ID), map[string]any{"carrier": "ZTO", "trackingNo": tracking}); err != nil {
		return err
	}
	if err := r.shows(ctx, "ship", "/projects/studio/orders/"+order.ID, tracking); err != nil {
		return err
	}
	if err := r.shows(ctx, "ship", "/projects/studio", tracking); err != nil {
		return err
	}
	afterDoc, err := r.get(ctx, other, nil)
	if err != nil {
		return err
	}
	if afterDoc == beforeDoc {
		return fmt.Errorf("ship: the document of %s, which lists the carriers, is the same after a parcel shipped", other)
	}
	if haveData {
		afterData, _, err := r.nav(ctx, other)
		if err != nil {
			return err
		}
		if afterData == beforeData {
			return fmt.Errorf("ship: the navigation data of %s, which lists the carriers, is the same after a parcel shipped", other)
		}
	}
	return nil
}

// verifyRename writes to each writable order, both kinds of the rename PATCH (the address alone, then
// the address and the name), and checks that the document of the order shows it and that so does
// the data a client navigation fetches for it.
func (r *runner) verifyRename(ctx context.Context) error {
	for _, fan := range []bool{false, true} {
		for _, w := range WritableOrders {
			n := int(r.writes.Add(1))
			st := WriteStep(r.write, w, n, fan)
			if err := Send(ctx, r.base, st); err != nil {
				return err
			}
			digits := fmt.Sprintf("%08d", n)
			url := "/projects/" + w.Project + "/orders/" + w.Order
			doc, err := r.get(ctx, url, nil)
			if err != nil {
				return err
			}
			if !strings.Contains(doc, digits) || fan && !strings.Contains(doc, st.Write.Customer) {
				return fmt.Errorf("the document of %s does not show write %d", w.Order, n)
			}
			data, ok, err := r.nav(ctx, url)
			if err != nil {
				return err
			}
			if ok && !strings.Contains(data, digits) {
				return fmt.Errorf("the navigation data of %s does not show write %d", w.Order, n)
			}
		}
	}
	return nil
}

// navRequest finds, in the traces, the data request of a click to a page that the browser let
// finish.
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

// ReadBrowseResult parses a result written by `compare browse`.
func ReadBrowseResult(data []byte) (*BrowseResult, error) {
	var r BrowseResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// raiseFileLimit lets the process open as many files as it may: the open model keeps thousands of
// connections open at high rates.
func raiseFileLimit() {
	var lim syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) == nil && lim.Cur < lim.Max {
		lim.Cur = lim.Max
		if lim.Cur > 1<<20 {
			lim.Cur = 1 << 20
		}
		syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
	}
}

// ConfirmRate looks for a rate at which the rule holds in all the rounds, starting at start and
// stepping down 10% for up to attempts tries. run plays one round at a rate. It returns the rate that
// passed all its rounds, or 0 if none did in range, and every round it ran, in order: a failing round
// ends its try, and the rounds of the failed tries are kept. If the first try passes, one step 10%
// above it is tried too, since the search can underestimate and nothing else would correct that: the
// rate above is published if it passes all its rounds, and the first one otherwise. The step up is
// not tried when it would pass maxRate, the cap of the search (none when 0).
func ConfirmRate(start float64, attempts, rounds int, run func(rate float64, round int) (*Round, error), maxRate float64) (float64, []Round, error) {
	var all []Round
	try := func(n int, rate float64) (bool, error) {
		for round := 1; round <= rounds; round++ {
			r, err := run(rate, round)
			if err != nil {
				return false, err
			}
			r.Round, r.Attempt = round, n
			all = append(all, *r)
			if !r.Pass {
				return false, nil
			}
		}
		return true, nil
	}
	rate := start
	for n := 1; n <= attempts; n++ {
		ok, err := try(n, rate)
		if err != nil {
			return 0, all, err
		}
		if ok {
			if up := math.Round(rate*1.1*100) / 100; n == 1 && (maxRate == 0 || up <= maxRate) {
				if ok, err := try(n+1, up); err != nil {
					return 0, all, err
				} else if ok {
					return up, all, nil
				}
			}
			return rate, all, nil
		}
		rate = math.Round(rate*0.9*100) / 100
	}
	return 0, all, nil
}

// IsBrowseResult says whether a JSON file is a result of `compare browse` rather than a record of
// `compare measure`.
func IsBrowseResult(data []byte) bool {
	var probe map[string]jsontext.Value
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	_, closed := probe["closed"]
	_, sustain := probe["sustain"]
	return closed && sustain
}

func (r *BrowseResult) json() ([]byte, error) { return json.Marshal(r) }
