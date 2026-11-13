package compare

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FormalBrowse are the settings of a formal run of the browse comparison, the defaults of `compare
// browse` and of browse.sh. A run with other settings, without the cpus of the server and of the load
// pinned, not run by browse.sh or without every server of BrowseFrameworks is not a formal run, and
// its report says so at the top, with how it differs.
var FormalBrowse = BrowseOptions{Sessions: 8, Rounds: 3, Sizing: 15 * time.Second, Target: 120 * time.Second, TrialWindow: 45 * time.Second, FinalWindow: 240 * time.Second, MaxRate: 16384, Seed: 1}

// BrowseOptions say how to run the browse comparison. The traces are the files `compare record`
// wrote, and the plans they were made from.
type BrowseOptions struct {
	TraceDir     string
	OutJSON      string // written after every round, and read to continue a run that stopped
	Seed         uint64
	Servers      []string
	ServerCPUs   string
	LoadCPUs     string
	LoadCPUCount int // how many cpus the load may use, to tell a saturated generator
	// ServerCPUCount is how many cpus the server is pinned to, to tell whether the sessions used them
	// (none when it is not pinned: then no server is stepped up).
	ServerCPUCount int
	// Locks are files the run holds an flock on while one of its servers runs, taken in this order
	// before the server starts and given back when it has stopped (holdLocks). They are for a
	// machine that other work shares: that work runs between the rounds, never in one.
	Locks []string
	// Announce is a folder in which the run leaves a file named by its pid from before it takes the
	// lock files of a round until it has given them back (announce): others on the machine start no
	// new heavy work while the file is there.
	Announce string
	// Hold is the command of a machine that keeps it quiet for a timing round, with its arguments,
	// run around each server of the run (holdWith). A machine that has one needs no Locks or Announce.
	Hold []string
	// Probe searches the rate of Next.js again, and writes it into RateFile.
	Probe bool
	// RateFile holds the confirmed rates of Next.js, per scenario (test/compare/rates.json when empty).
	RateFile    string
	Scenarios   []BrowseScenario // BrowseScenarios when empty (the tests set others)
	Phases      []string         // "throughput", "control", "sustain" and "latency"; all of them when empty
	Sessions    int              // the concurrent sessions every server starts the throughput at, 8
	Rounds      int
	Sizing      time.Duration // the length of a sizing run (browse.size)
	Target      time.Duration // how long a closed round should take, about
	TrialWindow time.Duration // an open trial of the search for the sustainable rate
	FinalWindow time.Duration // an open round of the latency table, at least
	MaxRate     float64       // the search for the sustainable rate stops here, visits a second (16384 when 0)
	Stamp       string        // what the traces and the builds belong to, kept in the result
	Log         io.Writer
}

// PlanInfo is what the plans of the site hold.
type PlanInfo struct {
	Blocks int `json:"blocks"`
	PlanCounts
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
	// Traces are the frameworks whose traces the run loaded: one for each framework of its servers.
	Traces    []string         `json:"traces,omitempty"`
	Plans     PlanInfo         `json:"plans"`
	Scenarios []BrowseScenario `json:"scenarios"`
	// Servers is, per scenario, the specs it runs, in the order of the tables.
	Servers map[string][]string `json:"servers"`
	// Closed holds every closed round, at every number of concurrent sessions, the invalid ones that
	// were run again too.
	Closed []Round `json:"closed"`
	// Blocks is the size of the measured workload of each scenario, number of sessions and spec (whole
	// blocks), taken from the sizing run of its first round.
	Blocks map[string]int `json:"blocks"`
	// Prepared is, per scenario, the visits whose data every server of the scenario gets before the
	// clock of a closed round (prepared): the same for all, whatever each was sized to. WarmVisits is
	// the visits of the warm-up of every closed round of the scenario, the ones after those, which get
	// their data too.
	Prepared   map[string]int `json:"prepared,omitempty"`
	WarmVisits map[string]int `json:"warmVisits,omitempty"`
	// Sustain is, per scenario and spec ("shop/tifa"), the search for the arrival rate it
	// sustains in the open model.
	Sustain map[string]*Sustain `json:"sustain"`
	// LatencyRate is, per scenario, the rate that all its servers were run at: half of the lowest
	// sustainable rate, which LatencyBase had.
	LatencyRate map[string]float64 `json:"latencyRate"`
	LatencyBase map[string]string  `json:"latencyBase"`
	Latency     []Round            `json:"latency"`
	// Control is the read-only control, on the first scenario: Tifa at the sessions of the closed runs with the
	// same traces and no writes, one short round, to show which pages its render memo answers.
	Control []Round `json:"control,omitempty"`
	// Throughput is, per scenario and server ("shop/tifa"), what the steps of its sessions found.
	Throughput map[string]*Throughput `json:"throughput,omitempty"`
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
	// Under is what the rate was measured under (see rateConditions), and From the run it came from
	// when it was read from the file of rates rather than searched in this run.
	Under map[string]string `json:"under,omitempty"`
	From  *RateSource       `json:"fromFile,omitempty"`
}

type browse struct {
	e     Env
	o     BrowseOptions
	res   *BrowseResult
	specs map[string]Spec
	// servers are the spec ids of each scenario.
	servers map[string][]string
	// set is the plans of the site, and trace their traces by the name a spec replays (traceName).
	set   *PlanSet
	trace map[string]map[int]Trace
	bid   string
	// deps are the products each page shows (store_deps.json), checked against the checkout.
	deps *StoreDeps
	// launch starts the server of a spec; the tests set it to serve from a fake (nil: start the real one).
	launch func(ctx context.Context, spec Spec) (*liveServer, error)
	// verify checks that the writes of a scenario show in the pages of a server; the tests replace it
	// (nil: runner.verifyWrites).
	verify func(ctx context.Context, r *runner) error
	// checked says which specs have had the writes of a scenario checked (key scenario/spec).
	checked map[string]bool
}

// liveServer is a running server of a spec, with what a round reads from it.
type liveServer struct {
	base string
	cpu  func() float64
	// cpuKnown says that cpu reads the processor time of the server (ReadsCPU), not a 0.
	cpuKnown bool
	rss      func() int64
	stop     func()
}

// start announces the round and takes the lock files of the run, waits for the cpus to be idle, starts a fresh server of a
// spec and waits until it listens. Stopping the server gives the lock files back and ends the announcement, so they are held
// for one server at a time: a round with its warm-up, a sizing run or a check.
func (b *browse) start(ctx context.Context, spec Spec) (*liveServer, error) {
	if b.launch != nil {
		return b.launch(ctx, spec)
	}
	unhold, err := holdWith(ctx, b.o.Hold)
	if err != nil {
		return nil, err
	}
	quiet, err := announce(b.o.Announce)
	if err != nil {
		unhold()
		return nil, err
	}
	unlocks, err := holdLocks(ctx, b.o.Locks)
	if err != nil {
		quiet()
		unhold()
		return nil, err
	}
	unlock := func() {
		unlocks()
		quiet()
		unhold()
	}
	if err := waitIdle(ctx, b.o.Log, b.o.ServerCPUs+","+b.o.LoadCPUs); err != nil {
		unlock()
		return nil, err
	}
	server, err := spec.Launch(b.o.ServerCPUs)
	if err != nil {
		unlock()
		return nil, err
	}
	stop := func() {
		server.Stop()
		unlock()
	}
	if err := server.WaitListening(ctx); err != nil {
		stop()
		return nil, err
	}
	return &liveServer{base: server.Base, cpu: server.CPUSeconds, cpuKnown: ReadsCPU(), rss: server.PeakRSS, stop: stop}, nil
}

// verifyWrites checks the writes of the scenario on the server of r.
func (b *browse) verifyWrites(ctx context.Context, r *runner) error {
	if b.verify != nil {
		return b.verify(ctx, r)
	}
	return r.verifyWrites(ctx)
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

// LoadTraces reads the plans of the site and the traces of the names given, and fails unless every
// name has a trace for every plan and for every variant of a plan: a replay on an incomplete set would not play the same visits on
// every server. It also fails for a trace the documents cannot be counted from (checkTrace), and unless
// the traces agree on which page views are documents (checkDocuments).
func LoadTraces(dir string, names []string) (*PlanSet, map[string]map[int]Trace, error) {
	set, err := ReadPlans(dir)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]map[int]Trace{}
	for _, name := range names {
		path := TracePath(dir, name)
		traces, dropped, err := ReadTraces(path)
		if err != nil {
			return nil, nil, err
		}
		// The forms of every plan, by plan and variant: the visit of the plan and each of its variants.
		forms := map[[2]int]Trace{}
		for _, t := range traces {
			forms[[2]int{t.Plan, t.Variant}] = t
		}
		visits := 0
		for _, p := range set.Plans {
			visits += 1 + len(p.Variants)
		}
		if dropped > 0 || len(forms) != visits {
			return nil, nil, fmt.Errorf("%s holds %d of the %d visits of the plans, the variants among them (%d visits failed to record): record again", path, len(forms), visits, dropped)
		}
		m := map[int]Trace{}
		for _, p := range set.Plans {
			var whole Trace
			for v := range 1 + len(p.Variants) {
				form, what := planForm(p, v), fmt.Sprintf("plan %d", p.ID)
				if v > 0 {
					what = fmt.Sprintf("variant %d of plan %d", v, p.ID)
				}
				t, ok := forms[[2]int{p.ID, v}]
				if !ok {
					return nil, nil, fmt.Errorf("%s has no trace for %s", path, what)
				}
				if len(t.Steps) != len(form.Steps) {
					return nil, nil, fmt.Errorf("%s: %s has %d page views, its trace %d", path, what, len(form.Steps), len(t.Steps))
				}
				// The landing parameter first, whose error says what of it is wrong, then the rest of the trace.
				if err := checkLanding(form, t); err != nil {
					return nil, nil, fmt.Errorf("%s: %w", path, err)
				}
				if err := checkTrace(form, t); err != nil {
					return nil, nil, fmt.Errorf("%s: %s: %w: record the traces again (`compare record`)", path, what, err)
				}
				if v == 0 {
					whole = t
					continue
				}
				// A variant is the visit with another word: the same acts, so the same page views load a
				// document. The plans count the documents of a block from the visits of its plans, whichever
				// form a played visit takes.
				if !slices.Equal(t.documents(), whole.documents()) {
					return nil, nil, fmt.Errorf("%s: %s loads a document at the page views %v and the visit of the plan at %v: record the traces again (`compare record`)", path, what, t.documents(), whole.documents())
				}
				whole.Variants = append(whole.Variants, t)
			}
			m[p.ID] = whole
		}
		out[name] = m
	}
	if err := checkDocuments(set, out, names); err != nil {
		return nil, nil, err
	}
	return set, out, nil
}

// checkTrace says whether the trace of a plan is one the documents of the plan can be counted from. It
// looks at the one trace, with nothing of another framework to set it beside, so it holds for a run
// that loads one framework too. The plans count one document for a page view whose trace has a
// document, and take it for a document of the page of the plan (Workload.docSteps, Expect). So:
//
//   - Every page view of the trace is the one of the plan: the same kind and the same page.
//   - A document of a page view is a GET of the page of the plan, with the same path and the same query
//     as a server reads it, and a page view has one at most.
//   - The landing has one.
//   - A page view whose act leaves the page no choice did what the act does (actLoads): a search, a
//     filter by a link and the next page of a list loaded a document, and the form of the sidebar and
//     the sort box loaded none.
//   - No document was cancelled. The plans count the documents the browser asked for and the replay
//     counts the answers it read whole, so the two are the same count only when every document was
//     read whole. Requests that are no documents may be cancelled, as Next.js does with its RSC
//     requests and its prefetches.
func checkTrace(p Plan, t Trace) error {
	if len(t.Steps) != len(p.Steps) {
		return fmt.Errorf("plan %d has %d page views, its trace %d", p.ID, len(p.Steps), len(t.Steps))
	}
	for i, s := range t.Steps {
		want := p.Steps[i]
		if s.Kind != want.Kind || s.Page != want.Page {
			return fmt.Errorf("plan %d: page view %d is a %s of %s in the plan and a %s of %s in the trace", p.ID, i, want.Kind, want.Page, s.Kind, s.Page)
		}
		docs := 0
		for _, q := range s.Reqs {
			if q.Class != "doc" {
				continue
			}
			if q.Cancel != "" {
				return fmt.Errorf("plan %d: the browser cancelled the document %s of page view %d (%s), so the plans cannot be set beside the answers", p.ID, q.URL, i, q.Cancel)
			}
			if q.Method != http.MethodGet || !samePage(q.URL, want.Page) {
				return fmt.Errorf("plan %d: page view %d (%s) has the document %s %s, which is not a GET of its page", p.ID, i, want.Page, q.Method, q.URL)
			}
			docs++
		}
		if docs > 1 {
			return fmt.Errorf("plan %d: page view %d (%s) has %d documents, and the plans count one for a page view", p.ID, i, want.Page, docs)
		}
		if i == 0 && docs == 0 {
			return fmt.Errorf("plan %d: page view 0, the landing on %s, has no document", p.ID, want.Page)
		}
		// The act of the page view, where it leaves the page no choice.
		switch loads, known := actLoads(want); {
		case known && loads && docs == 0:
			return fmt.Errorf("plan %d: page view %d (%s) is a %s, which loads a document, and the browser loaded none", p.ID, i, want.Page, want.Act)
		case known && !loads && docs > 0:
			return fmt.Errorf("plan %d: page view %d (%s) is a %s, which the router takes, and the browser loaded a document", p.ID, i, want.Page, want.Act)
		}
	}
	return nil
}

// samePage says whether a request URL is the page: the same path, and the same query as a server reads
// it, by whole names and whole values, whatever the order of the names.
func samePage(requested, page string) bool {
	got, err1 := url.Parse(requested)
	want, err2 := url.Parse(page)
	if err1 != nil || err2 != nil || got.Path != want.Path {
		return false
	}
	gotQuery, err1 := url.ParseQuery(got.RawQuery)
	wantQuery, err2 := url.ParseQuery(want.RawQuery)
	return err1 == nil && err2 == nil && reflect.DeepEqual(gotQuery, wantQuery)
}

// oneTraceNote says what a run that loaded the traces of names lacks when they are of one framework: the
// check that the frameworks agree on which page views are documents (checkDocuments) had nothing to
// compare. It is "" for more. The run goes on: each trace is still checked against its plan
// (checkTrace).
func oneTraceNote(names []string) string {
	if len(names) != 1 {
		return ""
	}
	return fmt.Sprintf("the traces of %s alone were loaded, so nothing shows that the frameworks agree on which page views are documents", names[0])
}

// checkDocuments fails unless the traces of every name agree, for every page view of every plan, on
// whether it was a document load. The pages are the same source in every framework, so a plain link or
// a form with no router loads a document in all of them, and a click the router takes in none. The
// plans count the documents of people from the traces (Workload.docSteps): the expected hit rate and
// the share of the crawlers are then the same whatever the framework whose trace is read. A set that
// does not agree is not one recording of the same pages.
func checkDocuments(set *PlanSet, traces map[string]map[int]Trace, names []string) error {
	if len(names) < 2 {
		return nil
	}
	first := names[0]
	for _, p := range set.Plans {
		for v := range 1 + len(p.Variants) {
			form := planForm(p, v)
			want := traces[first][p.ID].form(v).documents()
			for _, name := range names[1:] {
				for i, doc := range traces[name][p.ID].form(v).documents() {
					if doc != want[i] {
						is, not := name, first
						if want[i] {
							is, not = first, name
						}
						return fmt.Errorf("the traces do not agree on which page views are documents: page view %d of plan %d (%s) is a document load in %s and not in %s: record the traces again (`compare record`)", i, p.ID, form.Steps[i].Page, is, not)
					}
				}
			}
		}
	}
	return nil
}

// browseConfig is the settings of a run that its result keeps, and that a run must have to continue
// it: the options that change what a round plays or how it is judged, the cpu model of the machine,
// a hash of the trace and plan files the run loaded (traces) and the method (see methodHash).
func browseConfig(o BrowseOptions, cpu, traces, method string) map[string]string {
	return map[string]string{
		"sessions": fmt.Sprint(o.Sessions), "rounds": fmt.Sprint(o.Rounds), "sizing": o.Sizing.String(), "target": o.Target.String(),
		"trialWindow": o.TrialWindow.String(), "finalWindow": o.FinalWindow.String(), "serverCpus": o.ServerCPUs, "loadCpus": o.LoadCPUs,
		"probe": fmt.Sprint(o.Probe), "seed": fmt.Sprint(o.Seed), "maxRate": fmt.Sprint(cmp.Or(o.MaxRate, 16384)), "cpu": cpu, "traces": traces, "method": method,
	}
}

// canContinue says whether a result can be continued by a run with this stamp, settings (cfg, see
// browseConfig) and versions: all the same, so that a probe asked for again, other cpus, windows,
// traces, a seed, another machine or other versions never reuse the rounds of another run. The commit
// of Tifa is not compared: what of the checkout a round depends on is in the stamp, and in the
// conditions of a rate.
func canContinue(res *BrowseResult, stamp string, cfg, version map[string]string) error {
	if res.Stamp != stamp {
		return fmt.Errorf("belongs to other sources, plans or settings than this run:\n  file: %s\n  here: %s\nremove it to start again", res.Stamp, stamp)
	}
	for _, k := range sortedKeys(cfg) {
		if res.Config[k] != cfg[k] {
			return fmt.Errorf("was run with %s %q, and this run has %q: remove it to start again", k, res.Config[k], cfg[k])
		}
	}
	for _, k := range slices.Sorted(mapKeys(res.Version, version)) {
		if k != "tifa" && res.Version[k] != version[k] {
			return fmt.Errorf("was run with %s %q, and this run has %q: remove it to start again", k, res.Version[k], version[k])
		}
	}
	return nil
}

// restore reads back a result to continue, and checks it in one place before any phase runs, whatever
// the phases: its format, its settings and versions (canContinue, the method among them), and every
// rate it holds, searched or read from the file, against the conditions of this run and the build of
// Next.js that runs.
func (b *browse) restore(data []byte, cfg, version map[string]string) error {
	if err := checkFormat(data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, b.res); err != nil {
		return err
	}
	if err := canContinue(b.res, b.o.Stamp, cfg, version); err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(b.res.Sustain)) {
		name, fw, _ := strings.Cut(key, "/")
		i := slices.IndexFunc(b.res.Scenarios, func(s BrowseScenario) bool { return s.Name == name })
		if i < 0 {
			return fmt.Errorf("holds a rate of %s, which is not one of its scenarios: remove it to start again", name)
		}
		if diff := conditionDiff(b.res.Sustain[key].Under, b.rateConditions(b.res.Scenarios[i])); len(diff) > 0 {
			return fmt.Errorf("the rate of %s on %s was measured under other conditions than this run (%s): remove it to start again", BrowseLabel(fw), name, strings.Join(diff, "; "))
		}
		if err := b.checkNextBuild(); err != nil {
			return fmt.Errorf("holds a rate of %s: %w", name, err)
		}
	}
	return nil
}

// tracesHash is a short hash of the plan and trace files a run loads: the plans and the traces of
// names.
func tracesHash(dir string, names []string) string {
	h := sha256.New()
	paths := []string{PlansPath(dir)}
	for _, n := range slices.Sorted(slices.Values(names)) {
		paths = append(paths, TracePath(dir, n))
	}
	for _, path := range paths {
		fmt.Fprintf(h, "%s %s\n", filepath.Base(path), fileHash(path))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// Browse runs the browse comparison and returns what it measured. Prepare must have run.
func (e Env) Browse(ctx context.Context, o BrowseOptions) (*BrowseResult, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.MaxRate != 0 && o.MaxRate < minRate {
		return nil, fmt.Errorf("-max-rate %v: the search of the sustainable rate needs a cap of at least %v visits a second", o.MaxRate, minRate)
	}
	if len(o.Servers) == 0 {
		o.Servers = BrowseFrameworks
	}
	if len(o.Scenarios) == 0 {
		o.Scenarios = BrowseScenarios
	}
	if len(o.Phases) == 0 {
		o.Phases = []string{"throughput", "control", "sustain", "latency"}
	}
	if o.RateFile == "" {
		o.RateFile = filepath.Join(e.Root, "test", "compare", "rates.json")
	}
	if o.Sessions == 0 {
		o.Sessions = 8
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
	b := &browse{e: e, o: o, specs: map[string]Spec{}, servers: map[string][]string{}, trace: map[string]map[int]Trace{}}
	var specs []Spec
	var names []string // the traces the specs replay
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
			if !slices.Contains(names, traceName(s)) {
				names = append(names, traceName(s))
			}
		}
	}
	if b.set, b.trace, err = LoadTraces(o.TraceDir, names); err != nil {
		return nil, err
	}
	if note := oneTraceNote(names); note != "" {
		b.logf("%s\n", note)
	}
	if b.bid, err = e.tifaBuildID(); err != nil {
		return nil, err
	}
	// A store_deps.json that is not the one of this checkout stops the run: the expected hit rate would be of
	// other pages.
	if b.deps, err = CheckStoreDeps(e.Root); err != nil {
		return nil, err
	}
	mach, version := machine(), e.versions(specs)
	cfg := browseConfig(o, mach.CPU, tracesHash(o.TraceDir, names), methodHash(e.Root))
	b.res = &BrowseResult{}
	if data, err := os.ReadFile(o.OutJSON); err == nil && o.OutJSON != "" {
		if err := b.restore(data, cfg, version); err != nil {
			return nil, fmt.Errorf("%s: %w", o.OutJSON, err)
		}
		b.logf("continuing %s: %d closed rounds, %d latency rounds done\n", o.OutJSON, len(b.res.Closed), len(b.res.Latency))
	} else {
		b.res.Stamp, b.res.Date, b.res.Seed = o.Stamp, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), o.Seed
		b.res.Machine, b.res.Place, b.res.Version = mach, placement, version
		b.res.Scenarios, b.res.Servers, b.res.Traces = o.Scenarios, b.servers, names
		b.res.Plans = PlanInfo{Blocks: b.set.Blocks}
		for _, p := range b.set.Plans {
			b.res.Plans.PlanCounts.Add(p)
		}
		b.res.Config = cfg
		b.res.Blocks = map[string]int{}
		b.res.Prepared = map[string]int{}
		b.res.Sustain = map[string]*Sustain{}
		b.res.LatencyRate, b.res.LatencyBase = map[string]float64{}, map[string]string{}
	}
	b.logf("placement: %s\n", placement.Note)
	// No Node app may send an answer a cache could keep: the run stops before its first round if one
	// does.
	for _, s := range specs {
		if s.Framework != "tifa" {
			if err := b.checkNoCache(ctx, s, o.Scenarios[0]); err != nil {
				return b.res, err
			}
		}
	}
	has := func(p string) bool { return slices.Contains(o.Phases, p) }
	for _, phase := range []struct {
		name string
		run  func(context.Context, BrowseScenario) error
	}{{"throughput", b.throughput}, {"control", b.control}, {"sustain", b.sustain}, {"latency", b.latency}} {
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

// with starts a fresh server of a spec and calls f with a runner for the workload of the scenario.
// The server gets no request before f that renders a page: the check that the writes of the scenario
// show in its pages runs on a server of its own that is thrown away (checkWrites, once for a spec and a
// scenario), so that a server of Tifa starts its warm-up with an empty render memo. A run with writes
// first restocks the server (prepare), which renders nothing, for prepared visits, at least the visits
// it plays (-1 for both in a run that plays for a time, the sizing run), and the warm visits after
// them, which the warm-up before the clock plays (0 for none, as in a sizing run). f then makes the
// orders of the ships and the carts of the returning visitors of those visits before its warm-up
// (setup), which render nothing either, and plays the warm-up before its clock (warmUp): the same
// visits and writes on every server. A run whose restock fails is an invalid round, and f does not
// run. A run that can make more carts than the shop keeps (cartsFit) is an error before it sends
// anything. The round f returns is judged, and when
// it passed the writes are checked again, on the server it ran against, after its timing. The server is
// stopped after. Every round starts a fresh server, so the data is the same at the start of every round:
// the orders placed in one round are not there in the next.
func (b *browse) with(ctx context.Context, name string, sc BrowseScenario, visits, prepared, warm int, f func(*runner) *Round) (*Round, error) {
	// A round with no writes (the read-only control) has none to check, and writes nothing.
	if key := sc.Name + "/" + name; !sc.ReadOnly && !b.checked[key] {
		if err := b.checkWrites(ctx, name, sc); err != nil {
			return nil, err
		}
		if b.checked == nil {
			b.checked = map[string]bool{}
		}
		b.checked[key] = true
	}
	server, err := b.start(ctx, b.specs[name])
	if err != nil {
		return nil, err
	}
	defer server.stop()
	r, err := b.runnerFor(name, sc, server)
	if err != nil {
		return nil, err
	}
	// The data before the clock is made for prepared visits, at least the visits played, and for the
	// warm-up after them. The shop keeps shopMaxCarts carts: a run that can make more fails before it
	// sends anything.
	if prepared < 0 {
		prepared = sizingVisits(b.o.Sizing)
	}
	if visits > prepared {
		panic(fmt.Sprintf("a run of %d visits with the data of %d", visits, prepared))
	}
	r.prepared, r.warm = prepared, warm
	if err := r.wl.cartsFit(0, prepared+warm, shopMaxCarts); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", sc.Name, name, err)
	}
	if !sc.ReadOnly {
		if err := b.prepare(ctx, r, b.specs[name], visits, prepared+warm); err != nil {
			return &Round{Framework: name, Scenario: sc.Name, WhyInvalid: err.Error()}, nil
		}
	}
	out := f(r)
	// A run that was stopped (an interrupt) leaves a round that is not a result, and it is not kept.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out.Framework, out.Scenario = name, sc.Name
	out.Judge(sc)
	if out.Pass && !sc.ReadOnly {
		if err := b.verifyWrites(ctx, r); err != nil {
			return nil, fmt.Errorf("%s/%s, after the round: %w", sc.Name, name, err)
		}
	}
	return out, nil
}

// controlWindow is about how long the one round of the read-only control takes (the target of the
// closed rounds when that is shorter).
const controlWindow = 30 * time.Second

// controlled says whether the read-only control runs on a scenario of a list: on the first, once.
func controlled(list []BrowseScenario, sc BrowseScenario) bool {
	return len(list) > 0 && list[0].Name == sc.Name
}

// control is a diagnostic, not a result: once, one closed round of Tifa as it ships at the sessions
// of the closed runs, with the visits of the first scenario and no writes (the carts of the returning
// visitors are made, as they are data), after the warm-up of the closed rounds of the scenario with
// no writes either. So it differs from them in the writes alone. It shows which kinds of page the
// render memo answers when nothing changes, and against the rounds with writes what the writes do to
// it.
func (b *browse) control(ctx context.Context, sc BrowseScenario) error {
	if !controlled(b.o.Scenarios, sc) || !slices.Contains(b.servers[sc.Name], "tifa") || slices.ContainsFunc(b.res.Control, func(x Round) bool { return x.Scenario == sc.Name }) {
		return nil
	}
	reads := sc
	reads.ReadOnly = true
	window := min(b.o.Target, controlWindow)
	// The warm-up is the one of the closed rounds: the same visits, numbered after the prepared ones.
	prepared, warm, err := b.prepared(ctx, sc)
	if err != nil {
		return err
	}
	// The length of the round is sized on a server that is thrown away, with no writes.
	rate, err := b.size(ctx, "tifa", reads, b.o.Sessions)
	if err != nil {
		return err
	}
	visits, _ := playable(BlocksFor(rate, window, b.set.Blocks, reads.blockVisits())*reads.blockVisits(), prepared)
	out, err := b.with(ctx, "tifa", reads, visits, prepared, warm, func(r *runner) *Round {
		return r.RunClosed(ctx, b.o.Sessions, visits, 2*window)
	})
	if err != nil {
		return err
	}
	out.Model, out.Round = "control", 1
	b.res.Control = append(b.res.Control, *out)
	b.logf("control %-10s %2d sessions tifa, no writes: %.1f page views/s, renders by page %v, pass %v %s\n", sc.Name, out.Sessions, float64(out.PageViews)/out.Seconds, out.PageRenders, out.Pass, out.Why)
	return b.commit()
}

// prepareHeadroom is how many times the visits of the longest round a server of a scenario was sized
// to at the base sessions the data before the clock of its closed rounds is made for. A server that
// steps up to more sessions, or whose rounds are resized, can be sized to more visits: a round never
// plays more than the data was made for (Round.Capped).
const prepareHeadroom = 2

// prepared is how many visits of a scenario every server gets the data of before the clock of a
// closed round: the restock, the orders of the ships and the carts of the returning visitors. It is
// the same for every server of the scenario, so that each starts its rounds with the same data and
// is sent the same writes (an edit of the stock is worked out from what the restock gave): it is
// prepareHeadroom times the visits of the longest round a server of the scenario was sized to at the
// base sessions. Every server is sized first, and the number is kept in the result. It also returns
// the visits of the warm-up of every closed round of the scenario, at any sessions: as many as the
// longest round a server was sized to (prepared / prepareHeadroom), the visits after the prepared
// ones, whose data is made too.
func (b *browse) prepared(ctx context.Context, sc BrowseScenario) (prepared, warm int, err error) {
	if n := b.res.Prepared[sc.Name]; n > 0 {
		return n, b.res.WarmVisits[sc.Name], nil
	}
	most := 0
	for _, fw := range b.servers[sc.Name] {
		if err := b.sizeCell(ctx, fw, sc, b.o.Sessions); err != nil {
			return 0, 0, err
		}
		most = max(most, b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, fw)])
	}
	if b.res.Prepared == nil {
		b.res.Prepared = map[string]int{}
	}
	if b.res.WarmVisits == nil {
		b.res.WarmVisits = map[string]int{}
	}
	b.res.WarmVisits[sc.Name] = most * sc.blockVisits()
	b.res.Prepared[sc.Name] = prepareHeadroom * b.res.WarmVisits[sc.Name]
	b.logf("prepare %-10s the data of %d visits and a warm-up of the %d after them before the clock of every closed round, for every server\n", sc.Name, b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name])
	return b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name], b.commit()
}

// sizeCell sizes the closed rounds of a server at a number of sessions, once (size).
func (b *browse) sizeCell(ctx context.Context, fw string, sc BrowseScenario, sessions int) error {
	key := fmt.Sprintf("%s/%d/%s", sc.Name, sessions, fw)
	if _, ok := b.res.Blocks[key]; ok {
		return nil
	}
	rate, err := b.size(ctx, fw, sc, sessions)
	if err != nil {
		return err
	}
	b.res.Blocks[key] = BlocksFor(rate, b.o.Target, b.set.Blocks, sc.blockVisits())
	b.logf("size   %-10s %2d sessions %-12s %.2f visits/s with the writes of the mix: %d blocks\n", sc.Name, sessions, fw, rate, b.res.Blocks[key])
	return b.commit()
}

// closedRound plays one round of a cell, again (up to three tries) while it is not valid, and keeps
// every try.
func (b *browse) closedRound(ctx context.Context, fw string, sc BrowseScenario, sessions, round int) (*Round, error) {
	key := fmt.Sprintf("%s/%d/%s", sc.Name, sessions, fw)
	planBlocks := b.set.Blocks
	prepared, warm, err := b.prepared(ctx, sc)
	if err != nil {
		return nil, err
	}
	if err := b.sizeCell(ctx, fw, sc, sessions); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		// A round never plays more visits than every server gets the data of: one sized to more
		// plays them all, and is kept when that ends it early (calibrate).
		visits, capped := playable(b.res.Blocks[key]*sc.blockVisits(), prepared)
		out, err := b.with(ctx, fw, sc, visits, prepared, warm, func(r *runner) *Round {
			// A round that still runs too long (a server that slows down as its data grows) is
			// stopped at twice the target, which is too long to be valid, and resized from what it did.
			return r.RunClosed(ctx, sessions, visits, 2*b.o.Target)
		})
		if err != nil {
			return nil, err
		}
		out.Round, out.Attempt, out.Capped = round, attempt, capped
		if blocks := judgeClosed(out, b.o.Target, planBlocks, sc.blockVisits(), b.o.LoadCPUCount); blocks > 0 {
			b.res.Blocks[key] = blocks
		}
		b.res.Closed = append(b.res.Closed, *out)
		if err := b.commit(); err != nil {
			return nil, err
		}
		b.logf("closed %-10s %2d sessions %-12s round %d (try %d): %d blocks (capped %v), %.1f page views/s, %.2f ms CPU/page view, writes %.1f%%, %d failures, pass %v %s valid %v %s\n",
			sc.Name, sessions, fw, round, attempt, out.Blocks, out.Capped, float64(out.PageViews)/out.Seconds, 1000*out.CPUSeconds/math.Max(1, float64(out.PageViews)), 100*out.WriteShare, out.Failures, out.Pass, out.Why, out.Valid, out.WhyInvalid)
		if out.Valid || attempt >= maxTries {
			return out, nil
		}
	}
}

// size is how many visits a second a server completes with the mix of the scenario, writes
// included, on a fresh server with no warm-up that is then thrown away, for -sizing. The first round
// of a cell is sized from it. It plays sizingVisits visits at most, with their setup made before it
// as for a round. A sizing run that cannot size a round (sizingFault) is an error.
func (b *browse) size(ctx context.Context, fw string, sc BrowseScenario, sessions int) (float64, error) {
	var rate float64
	sized := &Round{}
	out, err := b.with(ctx, fw, sc, -1, -1, 0, func(r *runner) *Round {
		rate, sized = r.Size(ctx, sessions, b.o.Sizing, sizingVisits(b.o.Sizing))
		return &Round{}
	})
	if err == nil && !out.Valid && strings.HasPrefix(out.WhyInvalid, "restock") {
		return 0, fmt.Errorf("%s/%s, sizing: %s", sc.Name, fw, out.WhyInvalid)
	}
	if why := sizingFault(sized); err == nil && why != "" {
		return 0, fmt.Errorf("%s/%s, sizing: %s", sc.Name, fw, why)
	}
	return rate, err
}

// sizingFault says why a sizing run cannot size a round, or "" when it can: its setup failed, or the
// shop refused a request of it with 409 or 422, which the restock to the cap and the plan rule out,
// so the run did not play the mix. Failures of other kinds count in its rate (a 503 ends a visit fast).
func sizingFault(sized *Round) string {
	switch {
	case sized.SetupError != "":
		return "the setup: " + sized.SetupError
	case sized.Refused > 0:
		return fmt.Sprintf("the shop refused %d of its requests with 409 or 422", sized.Refused)
	}
	return ""
}

// sizingRate is the most visits a second a sizing run is made for: it plays sizingRate visits for
// each second of its length at most, so that the orders of their ships and the carts of their
// returning visitors can be made before it. A server faster than that ends the run early, and its
// rate is still the visits over the time they took.
const sizingRate = 1000

// sizingVisits is how many visits a sizing run of length d plays at most.
func sizingVisits(d time.Duration) int { return max(1, int(math.Ceil(sizingRate*d.Seconds()))) }

// judgeClosed finishes the judgement of a closed round: a load generator that ran out of cpus makes it
// invalid, whatever the server did (a closed round has no schedule, so only the cpus are checked, and
// only when the generator has cpus of its own, loadCPUs). Then its length is checked (calibrate), and
// it returns the blocks of the next round of its cell, 0 for no change.
func judgeClosed(out *Round, target time.Duration, planBlocks, blockVisits, loadCPUs int) int {
	if why := generatorFault(out, loadCPUs); why != "" {
		out.GeneratorLimited, out.Valid, out.WhyInvalid = true, false, why
	}
	return calibrate(out, target, planBlocks, blockVisits)
}

// calibrate checks the length of a closed round, which Judge has judged. A round takes 0.8 to 1.6
// times the target (96 to 192 seconds by default). Outside that it returns the blocks the next round
// of its cell gets, from the visits a second this one ended (the failed ones too: a 503 ends a visit
// fast, and the completed visits alone would size a server that refuses load far too small), and 0
// when the round was in the window. A round outside the window that passed the rule is invalid and is
// run again. One that failed the rule stays a valid round that failed: the length is a matter of
// calibration, and never discards a failure the server showed.
func calibrate(out *Round, target time.Duration, planBlocks, blockVisits int) int {
	lo, hi := roundLength(target)
	ended := out.Visits + out.VisitsFailed
	// A round that played every prepared visit cannot be made longer: short, it is kept as it is.
	if ended == 0 || (out.Seconds >= lo || out.Capped) && out.Seconds <= hi {
		return 0
	}
	if out.Pass && out.Valid {
		out.Valid, out.WhyInvalid = false, fmt.Sprintf("the round took %.0f s, outside %.0f to %.0f s: resized", out.Seconds, lo, hi)
	}
	return BlocksFor(float64(ended)/out.Seconds, target, planBlocks, blockVisits)
}

// generatorFault says why the load generator, not the server, limited a round, or "" when it did not:
// visits of an open round must start on time, and it must have processor time to spare (checked only
// when it has cpus of its own, loadCPUs).
func generatorFault(out *Round, loadCPUs int) string {
	why := ""
	if out.LagMs > 250 {
		why = fmt.Sprintf("the load generator started a visit %.0f ms late", out.LagMs)
	}
	if loadCPUs > 0 && out.Seconds > 0 && out.ClientCPU/out.Seconds > 0.8*float64(loadCPUs) {
		why = fmt.Sprintf("the load generator used %.2f cores of %d", out.ClientCPU/out.Seconds, loadCPUs)
	}
	return why
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

// openRound runs the open model on a fresh server: whole cycles of all the plans, after a warm-up of
// as many visits at the same rate with their writes, the visits after those of the round, apart.
func (b *browse) openRound(ctx context.Context, fw string, sc BrowseScenario, rate float64, window time.Duration) (*Round, error) {
	cycle := b.set.Blocks * sc.blockVisits()
	visits := visitsFor(rate, window, cycle)
	out, err := b.with(ctx, fw, sc, visits, visits, visits, func(r *runner) *Round {
		return r.RunOpen(ctx, rate, visits, 30*time.Second, 0)
	})
	if err != nil {
		return nil, err
	}
	// A generator that cannot keep up makes the round invalid, so that it is never reported as a limit
	// of the server.
	if why := generatorFault(out, b.o.LoadCPUCount); why != "" {
		out.GeneratorLimited, out.Valid, out.WhyInvalid = true, false, why
	}
	return out, nil
}

// sustainKey names the search of one spec in one scenario.
func sustainKey(sc, spec string) string { return sc + "/" + spec }

// maxRate is the cap of the search of the rate.
func (b *browse) maxRate() float64 { return cmp.Or(b.o.MaxRate, 16384) }

// sustain finds the arrival rate that Next.js sustains in the open model on a scenario with the open
// run: double the rate while the rule holds, narrow it to within 10%, then require the rule over the
// rounds, stepping down 10% until it holds. Only Next.js is searched: half of its rate is the rate of
// the open run for every server.
func (b *browse) sustain(ctx context.Context, sc BrowseScenario) error {
	if !sc.Latency {
		return nil
	}
	if !slices.Contains(b.servers[sc.Name], openBase) {
		return fmt.Errorf("%s: the open run is set by %s, which is not among the servers", sc.Name, BrowseLabel(openBase))
	}
	// A rate is reused, searched and written only on a build of the sources it is bound to.
	if err := b.checkNextBuild(); err != nil {
		return fmt.Errorf("%s: %w", sc.Name, err)
	}
	for _, fw := range []string{openBase} {
		key := sustainKey(sc.Name, fw)
		s := b.res.Sustain[key]
		if s != nil && s.Done {
			continue // restore checked it against the conditions of this run
		}
		under := b.rateConditions(sc)
		file, err := readRates(b.o.RateFile)
		if err != nil {
			return err
		}
		entry, err := fileRate(file, sc.Name, under, b.o.Probe)
		if err != nil {
			return err
		}
		if entry != nil {
			b.logf("sustain %-10s %-12s %.2f visits/s from %s, confirmed on %s (Tifa %s)\n", sc.Name, fw, entry.Rate, b.o.RateFile, entry.Source.Date, entry.Source.Tifa)
			b.res.Sustain[key] = &Sustain{Rate: entry.Rate, Done: true, Under: under, From: &entry.Source}
			if err := b.commit(); err != nil {
				return err
			}
			continue
		}
		s = &Sustain{Under: under}
		b.res.Sustain[key] = s
		// A trial that is not valid is played again at its rate: only a valid one moves the search.
		trial := func(rate float64) (string, error) {
			r, err := retryInvalid(func(try int) (*Round, error) {
				r, err := b.openRound(ctx, fw, sc, rate, b.o.TrialWindow)
				if err != nil {
					return nil, err
				}
				r.Attempt = try
				s.Trials = append(s.Trials, *r)
				b.logf("sustain %-10s %-12s %8.2f visits/s (try %d): pass %v %s valid %v %s (%d visits, load P99 %.0f, click P99 %.0f ms, client %.2f cores)\n", sc.Name, fw, rate, try, r.Pass, r.Why, r.Valid, r.WhyInvalid, r.Visits, r.Types["load"].P99, r.Types["click"].P99, r.ClientCPU/math.Max(r.Seconds, 1))
				return r, b.commit()
			})
			if err != nil {
				return "", err
			}
			return trialOf(r), nil
		}
		maxRate := b.maxRate()
		lo, hi, limited, err := searchRate(maxRate, trial)
		if err != nil {
			return err
		}
		s.Limited = limited
		if lo == 0 {
			s.Note, s.Done = fmt.Sprintf("it does not sustain %.2f visits a second, the lowest rate tried", hi), true
			if limited {
				s.Note = "the load generator could not play a valid trial, so the search found no rate"
			}
			if err := b.commit(); err != nil {
				return err
			}
			continue
		}
		for !s.Limited && hi > 0 && hi-lo > 0.1*lo {
			mid := math.Round((lo+hi)/2*100) / 100
			got, err := trial(mid)
			if err != nil {
				return err
			}
			switch got {
			case trialPass:
				lo = mid
			case trialFail:
				hi = mid
			default:
				s.Limited = true
			}
		}
		// Only a rate that passed all its confirmation rounds, each of them valid, is published. Every
		// round stays in the record.
		confirmed, rounds, limited, err := ConfirmRate(lo, 4, b.o.Rounds, func(rate float64, round int) (*Round, error) {
			r, err := b.openRound(ctx, fw, sc, rate, b.o.TrialWindow*2)
			if err != nil {
				return nil, err
			}
			b.logf("confirm %-10s %-12s %8.2f visits/s round %d: pass %v %s valid %v %s\n", sc.Name, fw, rate, round, r.Pass, r.Why, r.Valid, r.WhyInvalid)
			s.Confirm = append(s.Confirm, *r)
			return r, b.commit()
		}, maxRate)
		if err != nil {
			return err
		}
		s.Limited = s.Limited || limited
		s.Confirm = rounds
		s.Rate, s.Done = confirmed, true
		switch {
		case confirmed == 0 && limited:
			s.Note = fmt.Sprintf("the load generator could not play valid confirmation rounds at %.2f visits a second, so no rate is confirmed", lo)
		case confirmed == 0:
			s.Note = fmt.Sprintf("no sustainable rate in range: no rate from %.2f down in four steps of 10%% passed %d rounds", lo, b.o.Rounds)
		case hi == 0 && !s.Limited:
			s.Note = "the search reached its cap without failing"
		}
		if err := b.commit(); err != nil {
			return err
		}
		if err := b.keepRate(sc, s); err != nil {
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
	if !sc.Latency {
		return nil
	}
	if s := b.res.Sustain[sustainKey(sc.Name, openBase)]; s != nil && s.Done && s.Rate == 0 {
		// That is the result of the search: this scenario has no open run, and the others go on.
		b.logf("latency %s: no open run, %s has no sustainable rate in range (%s)\n", sc.Name, BrowseLabel(openBase), s.Note)
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
			// A round that is not valid (the load generator fell behind) is run again, up to three tries.
			for {
				tries := 0
				for _, x := range b.res.Latency {
					if x.Framework == fw && x.Scenario == sc.Name && x.Round == round {
						tries++
						if x.Valid {
							tries = maxTries
						}
					}
				}
				if tries >= maxTries {
					break
				}
				r, err := b.openRound(ctx, fw, sc, rate, window)
				if err != nil {
					return err
				}
				r.Round, r.Attempt = round, tries+1
				b.res.Latency = append(b.res.Latency, *r)
				b.logf("latency %-10s %-12s round %d (try %d): %d of %d visits succeeded, %d failed, %d unfinished, load P50 %.1f P99 %.1f, click P50 %.1f P99 %.1f ms, pass %v %s valid %v %s\n", sc.Name, fw, round, r.Attempt, r.Visits, r.VisitsPlanned, r.VisitsFailed, r.VisitsUnfinished, r.Types["load"].P50, r.Types["load"].P99, r.Types["click"].P50, r.Types["click"].P99, r.Pass, r.Why, r.Valid, r.WhyInvalid)
				if err := b.commit(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// browseSpecs are the specs a scenario runs.
func (e Env) browseSpecs(sc BrowseScenario, names []string) ([]Spec, error) {
	var specs []Spec
	for _, name := range names {
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
// The site has the writes of the storefront (verifyStore: the cart, and a checkout that the back
// office shows as an order), the edit of a product, which the storefront shows (verifyProduct), and
// the orders of the back office (verifyAdmin).
func (r *runner) verifyWrites(ctx context.Context) error {
	if err := r.verifyStore(ctx); err != nil {
		return err
	}
	if err := r.verifyProduct(ctx); err != nil {
		return err
	}
	return r.verifyAdmin(ctx)
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
// for it: the answers of every data request of the click, one after the other (TanStack Start, going
// from one project to another, asks for the project of the layout and then for the page). ok is
// false when no trace has a click to that page.
func (r *runner) nav(ctx context.Context, page string) (data string, ok bool, err error) {
	return r.navAs(ctx, http.DefaultClient, page)
}

// navAs is nav with a client of its own.
func (r *runner) navAs(ctx context.Context, c *http.Client, page string) (data string, ok bool, err error) {
	qs := r.navRequests(page)
	if len(qs) == 0 {
		return "", false, nil
	}
	var all strings.Builder
	for _, q := range qs {
		if r.buildID != "" {
			q.URL = tifaBuild.ReplaceAllString(q.URL, "_tifa="+r.buildID+".")
		}
		data, err := r.getAs(ctx, c, q.URL, q.Headers)
		if err != nil {
			return "", true, err
		}
		all.WriteString(data)
	}
	return all.String(), true, nil
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

// verifyAdmin makes each write of the admin mix once and checks what it changes.
//
//  1. Place an order with a note of its own: its page, the list of the project and the data of the
//     list show it.
//  2. Add a note to SH-1040, which has shipped (a note is allowed after shipping).
//  3. Change the recipient of an order that has not shipped.
//  4. Ship the order of step 1 with ZTO: its page and the list show the tracking number, the
//     carrier table of the stats page counts one more ZTO parcel, and the navigation data of the
//     stats page is not what it was.
//
// The data of an order that was placed here has no trace to ask a navigation request from, so it is
// read as a document. The order of step 1 is shipped in step 4.
func (r *runner) verifyAdmin(ctx context.Context) error {
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
	r.pool.countCreated("studio")
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

	// The recipient of an order of the data that waits, which no ship of a round takes (its page has a
	// trace).
	target, customer, phone := "SH-1041", WritableOrders[1].Customer, WritableOrders[1].Phone
	if _, err := call(http.MethodPatch, r.write("studio", target), map[string]any{"customer": customer, "phone": phone, "address": Address(n)}); err != nil {
		return err
	}
	if err := r.shows(ctx, "change the recipient", "/projects/studio/orders/"+target, digits); err != nil {
		return err
	}

	stats := "/projects/studio/stats"
	beforeDoc, err := r.get(ctx, stats, nil)
	if err != nil {
		return err
	}
	parcels, err := CarrierParcels(beforeDoc, "ZTO Express")
	if err != nil {
		return fmt.Errorf("ship: %s before the ship: %w", stats, err)
	}
	beforeData, haveData, err := r.nav(ctx, stats)
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
	afterDoc, err := r.get(ctx, stats, nil)
	if err != nil {
		return err
	}
	if n, err := CarrierParcels(afterDoc, "ZTO Express"); err != nil || n != parcels+1 {
		return fmt.Errorf("ship: the carrier table of %s counts %d ZTO parcels after one more shipped, want %d (%v)", stats, n, parcels+1, err)
	}
	if haveData {
		afterData, _, err := r.nav(ctx, stats)
		if err != nil {
			return err
		}
		if afterData == beforeData {
			return fmt.Errorf("ship: the navigation data of %s, which counts the parcels of each carrier, is the same after a parcel shipped", stats)
		}
	}
	return nil
}

// carrierCell is a line of the carrier table of the stats page as React renders it: the name of
// the carrier in a cell and the parcels it took in the next.
var carrierCell = regexp.MustCompile(`>([^<>]+)</td><td[^>]*>(\d+)</td>`)

// CarrierParcels reads from the document of a stats page how many parcels the carrier name took.
func CarrierParcels(doc, name string) (int, error) {
	var found []int
	for _, m := range carrierCell.FindAllStringSubmatch(doc, -1) {
		if m[1] == name {
			n, _ := strconv.Atoi(m[2])
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		return 0, fmt.Errorf("%d lines of the carrier table for %s", len(found), name)
	}
	return found[0], nil
}

// navRequests finds, in the traces, the first click to a page and returns its data requests that
// the browser let finish.
func (r *runner) navRequests(page string) []TraceReq {
	for _, p := range r.wl.Plans {
		for _, s := range r.wl.Traces[p.ID].Steps {
			if s.Kind != StepClick || s.Page != page {
				continue
			}
			var qs []TraceReq
			for _, q := range s.Reqs {
				if q.Class == "nav" && q.Cancel == "" && q.Method == "GET" && !strings.Contains(q.URL, "__manifest") {
					qs = append(qs, q)
				}
			}
			if len(qs) > 0 {
				return qs
			}
		}
	}
	return nil
}

// ReadBrowseResult parses a result written by `compare browse`. A result of the old format (users
// and not sessions, no planned visits) is refused: nothing reads it.
func ReadBrowseResult(data []byte) (*BrowseResult, error) {
	if err := checkFormat(data); err != nil {
		return nil, err
	}
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

// The outcomes of a round of the search of a rate: valid and passing the rule, valid and failing
// it, or not valid (the load generator did not do its work), which says nothing about the server.
const (
	trialPass    = "pass"
	trialFail    = "fail"
	trialInvalid = "invalid"
)

// trialOf is the outcome of a round.
func trialOf(r *Round) string {
	switch {
	case !r.Valid:
		return trialInvalid
	case r.Pass:
		return trialPass
	}
	return trialFail
}

// maxTries is how many times a round that is not valid is played, at the same rate or sessions.
const maxTries = 3

// retryInvalid plays a round (try counts from 1) until one is valid, up to maxTries, and returns the
// last one.
func retryInvalid(play func(try int) (*Round, error)) (*Round, error) {
	var r *Round
	for try := 1; try <= maxTries; try++ {
		var err error
		if r, err = play(try); err != nil {
			return nil, err
		}
		if r.Valid {
			break
		}
	}
	return r, nil
}

// ConfirmRate looks for a rate at which the rule holds in all the rounds, each of them valid, starting
// at start and stepping down 10% for up to attempts tries. run plays one round at a rate. A round that
// is not valid is played again at its rate (up to maxTries): it never steps the rate down. It returns
// the rate that passed all its rounds, or 0 if none did in range, every round it ran, in order (a
// failing round ends its try, and the rounds of the failed tries are kept), and whether the load
// generator limited it: a round stayed invalid, which ends the confirmation there. If the first try
// passes, one step 10% above it is tried too, since the search can underestimate and nothing else
// would correct that: the rate above is published if it passes all its rounds, and the first one
// otherwise. The step up is not tried when it would pass maxRate, the cap of the search (none when 0).
func ConfirmRate(start float64, attempts, rounds int, run func(rate float64, round int) (*Round, error), maxRate float64) (float64, []Round, bool, error) {
	var all []Round
	try := func(n int, rate float64) (string, error) {
		for round := 1; round <= rounds; round++ {
			r, err := retryInvalid(func(int) (*Round, error) {
				r, err := run(rate, round)
				if err != nil {
					return nil, err
				}
				r.Round, r.Attempt = round, n
				all = append(all, *r)
				return r, nil
			})
			if err != nil {
				return "", err
			}
			if got := trialOf(r); got != trialPass {
				return got, nil
			}
		}
		return trialPass, nil
	}
	rate := start
	for n := 1; n <= attempts; n++ {
		got, err := try(n, rate)
		if err != nil {
			return 0, all, false, err
		}
		switch got {
		case trialInvalid:
			return 0, all, true, nil
		case trialPass:
			if up := math.Round(rate*1.1*100) / 100; n == 1 && (maxRate == 0 || up <= maxRate) {
				got, err := try(n+1, up)
				if err != nil {
					return 0, all, false, err
				}
				if got == trialPass {
					return up, all, false, nil
				}
				return rate, all, got == trialInvalid, nil
			}
			return rate, all, false, nil
		}
		rate = math.Round(rate*0.9*100) / 100
	}
	return 0, all, false, nil
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

// minRate is the lowest cap of the search of the sustainable rate: half a visit a second.
const minRate = 0.5

// searchRate brackets the rate the rule holds at: it doubles from 4 visits a second (or from maxRate
// when that is lower) while the rule holds, or, if the first fails, halves until one holds. lo is the
// highest rate that passed (0 if none did) and hi the lowest that failed above it (0 if none did,
// within maxRate). A trial that stayed invalid ends the search where it is, limited by the load
// generator: it is neither a pass nor a failure of the server.
func searchRate(maxRate float64, trial func(rate float64) (string, error)) (lo, hi float64, limited bool, err error) {
	rate := min(4, maxRate)
	for rate <= maxRate {
		got, err := trial(rate)
		if err != nil {
			return 0, 0, false, err
		}
		if got == trialInvalid {
			return lo, hi, true, nil
		}
		if got == trialFail {
			hi = rate
			if lo == 0 && rate > minRate {
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
	return lo, hi, false, nil
}

// errOldFormat is the error for a result written by an older version of `compare browse`.
var errOldFormat = fmt.Errorf("written by an older version of compare browse (users and not sessions, or no planned visits): it cannot be reported or continued, run it again")

// checkFormat refuses a result of the old format: one whose settings or rounds count users, a round
// of which has no planned visits (closed, control, latency, or a trial or confirmation of a rate), or
// that has the saturation phase.
func checkFormat(data []byte) error {
	type rounds = []map[string]jsontext.Value
	var top struct {
		Settings map[string]string `json:"settings"`
		Closed   rounds            `json:"closed"`
		Control  rounds            `json:"control"`
		Latency  rounds            `json:"latency"`
		Sustain  map[string]struct {
			Trials  rounds `json:"trials"`
			Confirm rounds `json:"confirm"`
		} `json:"sustain"`
		Saturated  jsontext.Value `json:"saturated"`
		Saturation jsontext.Value `json:"saturation"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	if _, ok := top.Settings["users"]; ok || top.Saturated != nil || top.Saturation != nil {
		return errOldFormat
	}
	all := slices.Concat(top.Closed, top.Control, top.Latency)
	for _, s := range top.Sustain {
		all = slices.Concat(all, s.Trials, s.Confirm)
	}
	for _, x := range all {
		if _, users := x["users"]; users {
			return errOldFormat
		}
		if _, planned := x["visitsPlanned"]; !planned {
			return errOldFormat
		}
	}
	return nil
}

// runnerFor is a runner of the workload of a scenario against a server of a spec.
func (b *browse) runnerFor(name string, sc BrowseScenario, server *liveServer) (*runner, error) {
	spec := b.specs[name]
	wl, err := NewWorkload(b.set, b.trace[traceName(spec)], sc, b.o.Seed)
	if err != nil {
		return nil, err
	}
	r := &runner{base: server.base, write: spec.Write, create: spec.Create, storeWrite: spec.StoreWrite, products: spec.Products, editProduct: spec.EditProduct, metrics: spec.Metrics, wl: wl, seed: b.o.Seed, cpu: server.cpu, cpuKnown: server.cpuKnown, rss: server.rss, deps: b.deps}
	if spec.Framework == "tifa" {
		r.buildID = b.bid
	}
	return r, nil
}

// checkNoCache asks a fresh server of a spec, thrown away after, for the documents and the
// navigation data its traces replay in a scenario, and fails when an answer has a sign that a cache
// could keep it (CheckNoCache).
func (b *browse) checkNoCache(ctx context.Context, spec Spec, sc BrowseScenario) error {
	server, err := b.start(ctx, spec)
	if err != nil {
		return err
	}
	defer server.stop()
	r, err := b.runnerFor(spec.Name, sc, server)
	if err != nil {
		return err
	}
	reqs := slices.Concat(replayedGets(r.wl), crawlerGets())
	if err := CheckNoCache(ctx, r.base, reqs); err != nil {
		return fmt.Errorf("%s: %w", BrowseLabel(spec.Name), err)
	}
	b.logf("no cache   %-12s %d documents and navigation data asked twice, none a cache could keep\n", spec.Name, len(reqs))
	return nil
}

// checkWrites checks that the writes of a scenario show in the pages of a spec, on a fresh server that
// is thrown away, so that it is not done on the server of a round.
func (b *browse) checkWrites(ctx context.Context, name string, sc BrowseScenario) error {
	server, err := b.start(ctx, b.specs[name])
	if err != nil {
		return err
	}
	defer server.stop()
	r, err := b.runnerFor(name, sc, server)
	if err != nil {
		return err
	}
	if err := b.verifyWrites(ctx, r); err != nil {
		return fmt.Errorf("%s/%s: %w", sc.Name, name, err)
	}
	return nil
}

// playable is how many visits a closed round sized to sized visits plays, and whether that is every
// visit prepared for it (capped): it cannot be made longer.
func playable(sized, prepared int) (visits int, capped bool) {
	return min(sized, prepared), sized >= prepared
}
