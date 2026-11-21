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
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// FormalBrowse are the settings of a formal run of the browse comparison, the defaults of `compare
// browse` and of browse.sh. A run with other settings, without the cpus of the server and of the load
// pinned, not run by browse.sh or without every server of BrowseFrameworks is not a formal run, and
// its report says so at the top, with how it differs.
var FormalBrowse = BrowseOptions{Sessions: 8, Rounds: 3, Sizing: 15 * time.Second, Target: 120 * time.Second, LatencyBlocks: 3, Seed: 1}

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
	Hold      []string
	Scenarios []BrowseScenario // BrowseScenarios when empty (the tests set others)
	Phases    []string         // "throughput", "control" and "latency"; all of them when empty
	Sessions  int              // the concurrent sessions every server starts the throughput at, 8
	Rounds    int
	Sizing    time.Duration // the length of a sizing run (browse.size)
	Target    time.Duration // how long a closed round should take, about
	// LatencyBlocks is the blocks of the one latency round of every server, a multiple of the blocks of
	// the plans (3 when 0).
	LatencyBlocks int
	// Stamp is what every server of the run shares (the plans, the replayer, the toolchain and the
	// settings), kept in the result: a result of another stamp is not continued.
	Stamp string
	// ServerStamps are, per spec, what its results belong to besides Stamp: the trace it replays and
	// the build it runs. A result continued under another stamp of a server measures that server again.
	ServerStamps map[string]string
	// Calibrate times the machine with the fixed program of `compare calibrate` (CalibrateChild): before
	// the warm-up of every round, before its clock and after it, against the base of the run
	// (calibration.go). Nil: the run does not calibrate the machine, and is not formal.
	Calibrate func(ctx context.Context) (Calibration, error)
	Log       io.Writer
}

// ServerStamp is what the results of one server in a result belong to: the stamp browse.sh made of
// the trace it replays and the build it runs (BrowseOptions.ServerStamps), a hash of its trace file,
// and when it was first measured under them.
type ServerStamp struct {
	Stamp string `json:"stamp"`
	Trace string `json:"trace"`
	Date  string `json:"date"`
}

// PlanInfo is what the plans of the site hold.
type PlanInfo struct {
	Blocks int `json:"blocks"`
	PlanCounts
}

// BrowseResult is everything the browse comparison measured, round by round.
type BrowseResult struct {
	// Stamp is what every server shares, and ServerStamps what the results of each server belong to,
	// with the date it was measured.
	Stamp        string                 `json:"stamp"`
	ServerStamps map[string]ServerStamp `json:"serverStamps,omitempty"`
	Date         string                 `json:"date"`
	Seed         uint64                 `json:"seed"`
	Machine      Machine                `json:"machine"`
	Place        Placement              `json:"placement"`
	Version      map[string]string      `json:"versions"`
	Config       map[string]string      `json:"settings"`
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
	// LatencyRate is, per scenario, the rate that all its servers were run at: latencyShare of the
	// visits a second that LatencyBase (Next.js) completed in its closed rounds (latencyRate).
	LatencyRate map[string]float64 `json:"latencyRate"`
	LatencyBase map[string]string  `json:"latencyBase"`
	Latency     []Round            `json:"latency"`
	// Control is the read-only control, on the first scenario: Tifa at the sessions of the closed runs with the
	// same traces and no writes, one short round, to show which pages its render memo answers.
	Control []Round `json:"control,omitempty"`
	// Throughput is, per scenario and server ("shop/tifa"), what the steps of its sessions found.
	Throughput map[string]*Throughput `json:"throughput,omitempty"`
	// MachineChecks are the checks of the machine that passed, before a continued run measured a server
	// again (checkMachine).
	MachineChecks []MachineCheck `json:"machineChecks,omitempty"`
	// Calibration is the speed of the machine at the start of the run, which every round is timed
	// against (calibrationBase): shared like Prepared, and kept by a run that continues.
	Calibration *CalibrationBase `json:"calibration,omitempty"`
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
	// noCutoff is for tests of what a server is sent, whose result does not depend on how long a round
	// takes: no round is stopped at twice its target (the real run never sets it, see cutoff).
	noCutoff bool
	// anyLength is for tests of what happens around a closed round, whose fake server plays it in a
	// fraction of a second: a round of any length counts, so a slow moment of the machine that runs the
	// test cannot make a try not valid and add a try (the real run never sets it, see judgeClosed).
	anyLength bool
	// stale are the servers a continued result is measuring again (measureAgain), and playCheck plays
	// the round of the check of the machine in the tests (nil: checkRound plays it).
	stale     []string
	playCheck func(fw string, sc BrowseScenario, round, try int) (*Round, error)
	// hold takes the hold of a round in the tests (nil: takeHold takes the real one, and none when launch
	// is set).
	hold func(ctx context.Context) (func(), error)
}

// cutoff is how long a closed round of the target window may run before it is stopped: twice the
// target. Only a test can lift it (noCutoff).
func (b *browse) cutoff(window time.Duration) time.Duration {
	if b.noCutoff {
		return time.Hour
	}
	return 2 * window
}

// liveServer is a running server of a spec, with what a round reads from it.
type liveServer struct {
	base string
	cpu  func() float64
	// cpuKnown says that cpu reads the processor time of the server (ReadsCPU), not a 0.
	cpuKnown bool
	rss      func() int64
	stop     func()
	// calib is the calibration of the machine before the warm-up of the round, nil when the run does
	// not calibrate.
	calib *RoundCalibration
}

// takeHold takes the hold of a round: it announces the round, takes the lock files of the run and
// waits for the cpus to be idle. The function it returns gives them back. The tests take theirs (hold),
// and none when they launch fake servers.
func (b *browse) takeHold(ctx context.Context) (func(), error) {
	if b.hold != nil {
		return b.hold(ctx)
	}
	if b.launch != nil {
		return func() {}, nil
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
	return unlock, nil
}

// start takes the hold of a round (takeHold), starts a fresh server of a spec and waits until it
// listens. Stopping the server gives the hold back, so it is held for one server at a time: a round
// with its warm-up, a sizing run or a check. With gate, for a round, the machine is calibrated in the
// hold before the server starts (gateBeforeWarmUp), when the run calibrates.
func (b *browse) start(ctx context.Context, spec Spec, gate bool) (*liveServer, error) {
	unlock, err := b.takeHold(ctx)
	if err != nil {
		return nil, err
	}
	var calib *RoundCalibration
	if gate && b.calibrating() {
		if calib, unlock, err = b.gateBeforeWarmUp(ctx, unlock); err != nil {
			return nil, err
		}
	}
	if b.launch != nil {
		s, err := b.launch(ctx, spec)
		if err != nil {
			unlock()
			return nil, err
		}
		stop := s.stop
		s.stop, s.calib = func() { stop(); unlock() }, calib
		return s, nil
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
	return &liveServer{base: server.Base, cpu: server.CPUSeconds, cpuKnown: ReadsCPU(), rss: server.PeakRSS, stop: stop, calib: calib}, nil
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
// a hash of the plan file the run loaded (plans) and the method (see methodHash). The trace files are
// each server's own (ServerStamp).
func browseConfig(o BrowseOptions, cpu, plans, method string) map[string]string {
	return map[string]string{
		"sessions": fmt.Sprint(o.Sessions), "rounds": fmt.Sprint(o.Rounds), "sizing": o.Sizing.String(), "target": o.Target.String(),
		"latencyBlocks": fmt.Sprint(o.LatencyBlocks), "serverCpus": o.ServerCPUs, "loadCpus": o.LoadCPUs,
		"seed": fmt.Sprint(o.Seed), "cpu": cpu, "plans": plans, "method": method, "calibrate": onOff(o.Calibrate != nil),
	}
}

// engineVersion is the key of the version of the JavaScript engine in the versions of a result
// (Env.versions), which only Tifa runs.
const engineVersion = "quickjs"

// versionOwner is the framework whose servers a version of a result belongs to: the engine to Tifa,
// the package of a framework (whose key is the framework, Env.versions) to that framework, and "" to
// all the servers, for the toolchain they share (Go and Node). The commit of Tifa ("tifa") is no
// version of a server and is never compared: what of the checkout a server depends on is in its
// stamp.
func versionOwner(key string, frameworks []string) string {
	switch {
	case key == engineVersion:
		return "tifa"
	case key != "tifa" && slices.Contains(frameworks, key):
		return key
	}
	return ""
}

// canContinue says whether a result can be continued by a run with this shared stamp, settings (cfg,
// see browseConfig) and versions: the stamp, the settings and the versions every server shares (the
// toolchain) must be the same, so that other cpus, windows, plans, a seed,
// another machine or another toolchain never reuse the rounds of another run. A version that belongs
// to the servers of one framework (versionOwner) is not checked here: it measures those servers again
// (staleWhy). frameworks are the frameworks of the servers of the run.
func canContinue(res *BrowseResult, stamp string, cfg, version map[string]string, frameworks []string) error {
	if res.Stamp != stamp {
		return fmt.Errorf("belongs to other plans, replayer, toolchain or settings than this run:\n  file: %s\n  here: %s\nremove it to start again", res.Stamp, stamp)
	}
	for _, k := range sortedKeys(cfg) {
		if res.Config[k] != cfg[k] {
			return fmt.Errorf("was run with %s %q, and this run has %q: remove it to start again", k, res.Config[k], cfg[k])
		}
	}
	for _, k := range slices.Sorted(mapKeys(res.Version, version)) {
		if k != "tifa" && versionOwner(k, frameworks) == "" && res.Version[k] != version[k] {
			return fmt.Errorf("was run with %s %q, and this run has %q: remove it to start again", k, res.Version[k], version[k])
		}
	}
	return nil
}

// frameworks are the frameworks of the servers of the run, each once.
func (b *browse) frameworks() []string {
	var out []string
	for _, name := range b.serverNames() {
		if fw := b.specs[name].Framework; !slices.Contains(out, fw) {
			out = append(out, fw)
		}
	}
	return out
}

// staleWhy says why what a continued result holds of a server is not of this run, or "" when it is:
// the result does not know the server, the stamp of the server changed (the trace it replays or the
// build it runs), its trace file changed, or a version of its own (the engine of Tifa, the package of
// its framework, versionOwner) changed.
func (b *browse) staleWhy(name string, version map[string]string) string {
	kept, ok := b.res.ServerStamps[name]
	now := b.serverStamp(name)
	switch {
	case !ok:
		return "the result does not have it"
	case kept.Stamp != now.Stamp:
		return "its stamp changed (the trace it replays or the build it runs)"
	case kept.Trace != now.Trace:
		return "its trace file changed"
	}
	fw := b.specs[name].Framework
	for _, k := range slices.Sorted(mapKeys(b.res.Version, version)) {
		if versionOwner(k, b.frameworks()) == fw && b.res.Version[k] != version[k] {
			return fmt.Sprintf("it was run with %s %q, and this run has %q", k, b.res.Version[k], version[k])
		}
	}
	return ""
}

// forget drops all that a result holds of one server, so that it is measured again: its sizing, its
// closed rounds and the steps of its sessions, its latency rounds, and the control when it played it.
// The open rounds of every server of a scenario are played at a rate taken from the closed rounds of
// one server (LatencyBase, latencyRate), so when that server is dropped the latency rounds of every
// server of the scenario go with it, and the rate. It says what it dropped, "" for nothing.
func (r *BrowseResult) forget(name string) string {
	var what []string
	count := func(rounds *[]Round, of string) {
		n := len(*rounds)
		*rounds = slices.DeleteFunc(*rounds, func(x Round) bool { return x.Framework == name })
		if n -= len(*rounds); n > 0 {
			what = append(what, fmt.Sprintf("%d %s", n, of))
		}
	}
	count(&r.Closed, "closed rounds")
	count(&r.Latency, "latency rounds")
	count(&r.Control, "control rounds")
	sized := 0
	for key := range r.Blocks {
		if strings.HasSuffix(key, "/"+name) {
			delete(r.Blocks, key)
			sized++
		}
	}
	if sized > 0 {
		what = append(what, fmt.Sprintf("its sizing at %d numbers of sessions", sized))
	}
	for key := range r.Throughput {
		if strings.HasSuffix(key, "/"+name) {
			delete(r.Throughput, key)
			what = append(what, "the steps of its sessions")
		}
	}
	for sc, base := range r.LatencyBase {
		if base != name {
			continue
		}
		n := len(r.Latency)
		r.Latency = slices.DeleteFunc(r.Latency, func(x Round) bool { return x.Scenario == sc })
		delete(r.LatencyRate, sc)
		delete(r.LatencyBase, sc)
		what = append(what, fmt.Sprintf("the rate of the latency rounds of %s, %.0f%% of its closed throughput, and the %d latency rounds of the other servers played at it", sc, 100*latencyShare, n-len(r.Latency)))
	}
	return strings.Join(what, ", ")
}

// measureAgain drops what a continued result holds of every server that is not of this run (staleWhy)
// and stamps it anew, keeping its versions of its own, and says so. The other servers are untouched,
// and so are the numbers every server of a scenario shares (the prepared visits and the warm-up, see
// prepared), unless no server of the scenario is kept. It returns the servers it measures again.
func (b *browse) measureAgain(version map[string]string) []string {
	var stale []string
	for _, name := range b.serverNames() {
		if why := b.staleWhy(name, version); why != "" {
			stale = append(stale, name)
			dropped := cmp.Or(b.res.forget(name), "nothing, it had no results yet")
			b.logf("measuring %s again: %s. Dropped: %s\n", name, why, dropped)
		}
	}
	for _, name := range stale {
		b.stampServer(name)
		fw := b.specs[name].Framework
		for _, k := range slices.Sorted(mapKeys(b.res.Version, version)) {
			if versionOwner(k, b.frameworks()) != fw {
				continue
			}
			if v, ok := version[k]; ok {
				b.res.Version[k] = v
			} else {
				delete(b.res.Version, k)
			}
		}
	}
	for _, sc := range b.o.Scenarios {
		if len(stale) > 0 && !slices.ContainsFunc(b.servers[sc.Name], func(name string) bool { return !slices.Contains(stale, name) }) {
			delete(b.res.Prepared, sc.Name)
			delete(b.res.WarmVisits, sc.Name)
		}
	}
	return stale
}

// restore reads back a result to continue, and checks it in one place before any phase runs, whatever
// the phases: its format, its shared stamp, settings and versions (canContinue, the method among
// them). It then drops what it holds of the servers that are not of this run (measureAgain).
func (b *browse) restore(data []byte, cfg, version map[string]string) error {
	if err := checkFormat(data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, b.res); err != nil {
		return err
	}
	if err := canContinue(b.res, b.o.Stamp, cfg, version, b.frameworks()); err != nil {
		return err
	}
	if b.res.Version == nil {
		b.res.Version = map[string]string{}
	}
	b.stale = b.measureAgain(version)
	return nil
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
		o.Scenarios = BrowseScenarios
	}
	if len(o.Phases) == 0 {
		o.Phases = []string{"throughput", "control", "latency"}
	}
	if o.Sessions == 0 {
		o.Sessions = 8
	}
	if o.Rounds == 0 {
		o.Rounds = 3
	}
	if o.LatencyBlocks == 0 {
		o.LatencyBlocks = 3
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
	cfg := browseConfig(o, mach.CPU, fileHash(PlansPath(o.TraceDir)), methodHash(e.Root))
	b.res = &BrowseResult{}
	if data, err := os.ReadFile(o.OutJSON); err == nil && o.OutJSON != "" {
		if err := b.restore(data, cfg, version); err != nil {
			return nil, fmt.Errorf("%s: %w", o.OutJSON, err)
		}
		b.logf("continuing %s: %d closed rounds, %d latency rounds done\n", o.OutJSON, len(b.res.Closed), len(b.res.Latency))
	} else {
		b.begin(cfg, version, mach, placement, names)
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
	// The speed of the machine every round is timed against: a run that continues keeps the base it finds.
	if err := b.calibrationBase(ctx); err != nil {
		return b.res, err
	}
	// A continued run that measures a server again first checks that the machine is as fast as it was
	// for the rounds it keeps.
	if err := b.checkMachine(ctx, b.stale); err != nil {
		return b.res, err
	}
	has := func(p string) bool { return slices.Contains(o.Phases, p) }
	for _, phase := range []struct {
		name string
		run  func(context.Context, BrowseScenario) error
	}{{"throughput", b.throughput}, {"control", b.control}, {"latency", b.latency}} {
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

// begin starts a new result: the stamps and settings of the run, the machine, the plans, and the stamp
// of every server with today's date.
func (b *browse) begin(cfg, version map[string]string, mach Machine, placement Placement, names []string) {
	b.res.Stamp, b.res.Date, b.res.Seed = b.o.Stamp, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), b.o.Seed
	b.res.Machine, b.res.Place, b.res.Version = mach, placement, version
	b.res.Scenarios, b.res.Servers, b.res.Traces = b.o.Scenarios, b.servers, names
	b.res.Plans = PlanInfo{Blocks: b.set.Blocks}
	for _, p := range b.set.Plans {
		b.res.Plans.PlanCounts.Add(p)
	}
	b.res.Config = cfg
	b.res.Blocks = map[string]int{}
	b.res.Prepared = map[string]int{}
	b.res.LatencyRate, b.res.LatencyBase = map[string]float64{}, map[string]string{}
	for _, name := range b.serverNames() {
		b.stampServer(name)
	}
}

// serverNames are the specs of every scenario of the run, each once, in order.
func (b *browse) serverNames() []string {
	var out []string
	for _, sc := range b.o.Scenarios {
		for _, name := range b.servers[sc.Name] {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// serverStamp is what the results of a spec belong to in this run (ServerStamp), without the date.
func (b *browse) serverStamp(name string) ServerStamp {
	return ServerStamp{Stamp: b.o.ServerStamps[name], Trace: fileHash(TracePath(b.o.TraceDir, traceName(b.specs[name])))}
}

// stampServer keeps in the result what the results of a spec belong to from now on, with today's
// date: the date it was measured.
func (b *browse) stampServer(name string) {
	if b.res.ServerStamps == nil {
		b.res.ServerStamps = map[string]ServerStamp{}
	}
	s := b.serverStamp(name)
	s.Date = time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	b.res.ServerStamps[name] = s
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
	return b.replayWhenOff(func() (*Round, error) { return b.withOnce(ctx, name, sc, visits, prepared, warm, f) })
}

// withOnce plays a round on a fresh server, as with says, once. When the run calibrates the machine,
// the round keeps its calibrations, and one off the base before the clock or after the round is not
// judged: it is not valid for the reason "machine", and with plays it again.
func (b *browse) withOnce(ctx context.Context, name string, sc BrowseScenario, visits, prepared, warm int, f func(*runner) *Round) (*Round, error) {
	server, err := b.start(ctx, b.specs[name], true)
	if err != nil {
		return nil, err
	}
	defer server.stop()
	r, err := b.runnerFor(name, sc, server)
	if err != nil {
		return nil, err
	}
	if b.calibrating() {
		r.calibrate = func(when string) (RoundCalibration, error) { return b.probe(ctx, when) }
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
	if r.calibErr != nil {
		return nil, r.calibErr
	}
	if server.calib != nil {
		out.Calibrations = append([]RoundCalibration{*server.calib}, out.Calibrations...)
	}
	if off := machineOff(out.Calibrations); off != "" {
		out.Valid, out.WhyInvalid = false, "machine: "+off
		return out, nil
	}
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
		return r.RunClosed(ctx, b.o.Sessions, visits, b.cutoff(window))
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
//
// A continued result keeps the numbers it holds when it measures a server again (measureAgain): the
// servers it keeps were measured with them. A server with no sizing at the base sessions (one measured
// again) is sized then, and must fit in the warm-up kept: the numbers never grow, and a server that
// needs more stops the run, which must start again.
func (b *browse) prepared(ctx context.Context, sc BrowseScenario) (prepared, warm int, err error) {
	if n := b.res.Prepared[sc.Name]; n > 0 {
		warm := b.res.WarmVisits[sc.Name]
		for _, fw := range b.servers[sc.Name] {
			if err := b.sizeCell(ctx, fw, sc, b.o.Sessions, warm); err != nil {
				return 0, 0, err
			}
		}
		return n, warm, nil
	}
	most := 0
	for _, fw := range b.servers[sc.Name] {
		if err := b.sizeCell(ctx, fw, sc, b.o.Sessions, 0); err != nil {
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

// sizeCell sizes the closed rounds of a server at a number of sessions, once (size). A size of more
// visits than most (0 for no limit), the warm-up a continued result keeps, is an error and is not
// kept, so that the run stops there every time it is continued.
func (b *browse) sizeCell(ctx context.Context, fw string, sc BrowseScenario, sessions, most int) error {
	key := fmt.Sprintf("%s/%d/%s", sc.Name, sessions, fw)
	if _, ok := b.res.Blocks[key]; ok {
		return nil
	}
	rate, err := b.size(ctx, fw, sc, sessions)
	if err != nil {
		return err
	}
	blocks := BlocksFor(rate, b.o.Target, b.set.Blocks, sc.blockVisits())
	if most > 0 && blocks*sc.blockVisits() > most {
		return fmt.Errorf("%s/%s, measured again, is sized to %d visits a round at %d sessions (%.2f visits/s), more than the warm-up of %d visits that every server of this result got. The visits prepared and the warm-up are the same for every server and do not grow, so the whole run must start again: remove %s", sc.Name, fw, blocks*sc.blockVisits(), sessions, rate, most, cmp.Or(b.o.OutJSON, "the result"))
	}
	b.res.Blocks[key] = blocks
	b.logf("size   %-10s %2d sessions %-12s %.2f visits/s with the writes of the mix: %d blocks\n", sc.Name, sessions, fw, rate, b.res.Blocks[key])
	return b.commit()
}

// closedRound plays one round of a cell, again (up to three tries) while it is not valid, and keeps
// every try.
func (b *browse) closedRound(ctx context.Context, fw string, sc BrowseScenario, sessions, round int) (*Round, error) {
	key := fmt.Sprintf("%s/%d/%s", sc.Name, sessions, fw)
	prepared, warm, err := b.prepared(ctx, sc)
	if err != nil {
		return nil, err
	}
	if err := b.sizeCell(ctx, fw, sc, sessions, 0); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		out, blocks, err := b.closedTry(ctx, fw, sc, sessions, b.res.Blocks[key], prepared, warm)
		if err != nil {
			return nil, err
		}
		out.Round, out.Attempt = round, attempt
		if blocks > 0 {
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

// closedTry plays one try of a closed round of a cell sized to blocks, as every round of the cell is
// played: its setup, its warm-up and the round on a fresh server (with), then its judgement
// (judgeClosed). It returns the try and the blocks of the next try of the cell, 0 for no change, and
// keeps nothing in the result.
func (b *browse) closedTry(ctx context.Context, fw string, sc BrowseScenario, sessions, blocks, prepared, warm int) (*Round, int, error) {
	// A round never plays more visits than every server gets the data of: one sized to more plays them
	// all, and is kept when that ends it early (calibrate).
	visits, capped := playable(blocks*sc.blockVisits(), prepared)
	out, err := b.with(ctx, fw, sc, visits, prepared, warm, func(r *runner) *Round {
		// A round that still runs too long (a server that slows down as its data grows) is stopped at
		// twice the target, which is too long to be valid, and resized from what it did.
		return r.RunClosed(ctx, sessions, visits, b.cutoff(b.o.Target))
	})
	if err != nil {
		return nil, 0, err
	}
	out.Capped = capped
	if b.anyLength {
		return out, 0, nil
	}
	return out, judgeClosed(out, b.o.Target, b.set.Blocks, sc.blockVisits(), b.o.LoadCPUCount), nil
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

// maxStartLagMs is how late, in milliseconds, a visit of an open round may start before the load
// generator is blamed for it. Tests whose subject is not the generator can raise it, since how
// soon a goroutine runs on a busy test machine is not theirs to check.
var maxStartLagMs = 250.0

// generatorFault says why the load generator, not the server, limited a round, or "" when it did not:
// visits of an open round must start on time, and it must have processor time to spare (checked only
// when it has cpus of its own, loadCPUs).
func generatorFault(out *Round, loadCPUs int) string {
	why := ""
	if out.LagMs > maxStartLagMs {
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
				if q.Class == "nav" && q.Cancel == "" && q.Method == "GET" {
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

// IsBrowseResult says whether a JSON file is a result of `compare browse` rather than a record of
// `compare measure`.
func IsBrowseResult(data []byte) bool {
	var probe map[string]jsontext.Value
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	_, closed := probe["closed"]
	_, latency := probe["latency"]
	return closed && latency
}

func (r *BrowseResult) json() ([]byte, error) { return json.Marshal(r) }

// errOldFormat is the error for a result written by an older version of `compare browse`.
var errOldFormat = fmt.Errorf("written by an older version of compare browse (users and not sessions, no planned visits, or a search of the rate Next.js sustains): it cannot be reported or continued, run it again")

// checkFormat refuses a result of an old format: one whose settings or rounds count users, a round
// of which has no planned visits (closed, control or latency), or that has the saturation phase or
// the search of the rate Next.js sustains, which set the latency rate before it came from the closed
// rounds of Next.js.
func checkFormat(data []byte) error {
	type rounds = []map[string]jsontext.Value
	var top struct {
		Settings   map[string]string `json:"settings"`
		Closed     rounds            `json:"closed"`
		Control    rounds            `json:"control"`
		Latency    rounds            `json:"latency"`
		Sustain    jsontext.Value    `json:"sustain"`
		Saturated  jsontext.Value    `json:"saturated"`
		Saturation jsontext.Value    `json:"saturation"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	if _, ok := top.Settings["users"]; ok || top.Saturated != nil || top.Saturation != nil || top.Sustain != nil {
		return errOldFormat
	}
	for _, x := range slices.Concat(top.Closed, top.Control, top.Latency) {
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
	server, err := b.start(ctx, spec, false)
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
	server, err := b.start(ctx, b.specs[name], false)
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
