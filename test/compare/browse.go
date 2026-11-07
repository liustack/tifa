package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// BrowseOptions say how to run the browse comparison. Traces are the files `compare record`
// wrote for each framework, and the plans they were made from.
type BrowseOptions struct {
	TraceDir     string
	Seed         uint64
	Scenarios    []BrowseScenario
	Servers      []string // spec ids; the four frameworks when empty
	ServerCPUs   string
	LoadCPUs     string
	Rounds       int
	SearchWarm   time.Duration
	SearchWindow time.Duration
	Warm         time.Duration
	Window       time.Duration
	MaxUsers     int      // the search stops here
	From         *Record  // a record whose runs are kept, to rerun some of its rounds
	Only         []string // scenario/spec keys to run: with From, the ones whose rounds run again
	Source       string   // names this run in the runs it adds
	Check        bool     // also count what each server answered in one round of the baseline
	Log          io.Writer
}

// BrowseFrameworks are the specs the browse comparison runs: each framework as shipped.
var BrowseFrameworks = []string{"tifa", "nextjs", "start", "reactrouter"}

// CurvePoint is one step of the search for how many users a framework carries.
type CurvePoint struct {
	Users      int     `json:"users"`
	VisitsPerS float64 `json:"visitsPerSecond"`
	Errors     int     `json:"errors"`
	DocP99     float64 `json:"fullLoadP99Ms"`
	NavP99     float64 `json:"navigationP99Ms"`
	Pass       bool    `json:"pass"`
	// Visits is how many visits the step completed, OpenOldest how long the oldest request still
	// open at the end of the window had waited (ms), and Why what failed the step.
	Visits     int     `json:"visits"`
	OpenOldest float64 `json:"openOldestMs"`
	Why        string  `json:"why,omitempty"`
	// Orders is how many orders each project held at the end of the step. The steps of a search share
	// one server, so the orders placed in the steps before count.
	Orders map[string]int `json:"orders,omitempty"`
}

// BrowseRun is one framework in one round of one scenario, at the users the search chose.
type BrowseRun struct {
	Scenario  string      `json:"scenario"`
	Spec      string      `json:"spec"`
	Framework string      `json:"framework"`
	Label     string      `json:"label"`
	Round     int         `json:"round"`
	Source    string      `json:"source,omitempty"`
	Load      *LoadResult `json:"load"`
}

// BrowseData is what `compare browse` adds to the record.
type BrowseData struct {
	Seed      uint64                  `json:"seed"`
	Traces    map[string]int          `json:"traces"` // by file name: the visits recorded
	Dropped   map[string]int          `json:"dropped,omitempty"`
	Curves    map[string][]CurvePoint `json:"curves"` // by scenario/spec
	Chosen    map[string]int          `json:"chosen"`
	Scenarios []BrowseScenario        `json:"scenarios"`
	Runs      []BrowseRun             `json:"runs"`
	// Rejected are the rounds that failed the rule of the search, kept for the record.
	Rejected []BrowseRun `json:"rejected,omitempty"`
	// Attempts say, per scenario/spec, which step the rounds were run at and whether they passed.
	Attempts   map[string][]Attempt   `json:"attempts,omitempty"`
	MaxUsers   int                    `json:"maxUsers,omitempty"`
	TraceCheck map[string]*TraceCheck `json:"traceCheck,omitempty"`
	// Common is, per visit length, how many plans have a trace for every server: those are the
	// visits that are replayed. Plans is how many plans there are.
	Common       map[string]int `json:"commonPlans,omitempty"`
	Plans        int            `json:"plans,omitempty"`
	Warm         string         `json:"warm"`
	Window       string         `json:"window"`
	SearchWindow string         `json:"searchWindow"`
}

// pass is the rule for a step of the search: no errors, and the P99 of a full load and of a whole
// navigation under a second.
func (c CurvePoint) passes() bool { return c.why() == "" }

// why says what fails a step: no visit completed (a server that hangs completes none and has no
// errors), a request still waiting for longer than a second when the window ended, errors, or a
// P99 of a full load or of a whole navigation over a second.
func (c CurvePoint) why() string {
	switch {
	case c.Visits == 0:
		return "no visit completed"
	case c.OpenOldest > 1000:
		return fmt.Sprintf("a request had waited %.0f ms at the end of the window", c.OpenOldest)
	case c.Errors > 0:
		return fmt.Sprintf("%d errors", c.Errors)
	case c.DocP99 > 1000:
		return fmt.Sprintf("full load P99 %.0f ms", c.DocP99)
	case c.NavP99 > 1000:
		return fmt.Sprintf("navigation P99 %.0f ms", c.NavP99)
	}
	return ""
}

type browseSet struct {
	plans  []Plan
	traces map[int]Trace
	file   string
	count  int
	drop   int
}

func loadBrowseSet(dir, spec string, mean int) (*browseSet, error) {
	var plans []Plan
	data, err := os.ReadFile(PlansPath(dir, mean))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, err
	}
	file := TracePath(dir, spec, mean)
	traces, dropped, err := ReadTraces(file)
	if err != nil {
		return nil, err
	}
	set := &browseSet{plans: plans, traces: map[int]Trace{}, file: filepath.Base(file), count: len(traces), drop: dropped}
	for _, t := range traces {
		set.traces[t.Plan] = t
	}
	return set, nil
}

// loadSets loads the traces of every spec for a visit length and keeps, in each, only the plans
// that all of them have a trace for, so that every server replays the same visits. It returns how
// many plans that is.
func loadSets(dir string, specs []Spec, mean int) (map[string]*browseSet, int, error) {
	sets := map[string]*browseSet{}
	var common map[int]bool
	for _, s := range specs {
		set, err := loadBrowseSet(dir, s.Name, mean)
		if err != nil {
			return nil, 0, err
		}
		sets[s.Name] = set
		if common == nil {
			common = map[int]bool{}
			for id := range set.traces {
				common[id] = true
			}
			continue
		}
		for id := range common {
			if _, ok := set.traces[id]; !ok {
				delete(common, id)
			}
		}
	}
	if len(common) < 100 {
		return nil, 0, fmt.Errorf("only %d plans have a trace for all of %d servers (mean %d), too few for a replay that does not loop over the same few", len(common), len(specs), mean)
	}
	for _, set := range sets {
		for id := range set.traces {
			if !common[id] {
				delete(set.traces, id)
			}
		}
	}
	return sets, len(common), nil
}

// Browse runs the browse comparison and returns a record that holds it. Prepare must have run, and
// the traces must have been recorded against these builds.
func (e Env) Browse(ctx context.Context, o BrowseOptions) (*Record, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if len(o.Servers) == 0 {
		o.Servers = BrowseFrameworks
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
	var specs []Spec
	for _, name := range o.Servers {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no spec named %q", name)
		}
		specs = append(specs, e.Specs()[i])
	}
	buildID, err := e.tifaBuildID()
	if err != nil {
		return nil, err
	}
	rec := o.From
	if rec == nil {
		rec = &Record{
			Date: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), Versions: e.versions(specs), Machine: machine(), Placement: placement,
			Settings: Settings{Rounds: o.Rounds, Steady: o.Window.String(), AcceptEncoding: "gzip"},
			Browse: &BrowseData{Seed: o.Seed, Traces: map[string]int{}, Dropped: map[string]int{}, Curves: map[string][]CurvePoint{}, Chosen: map[string]int{},
				Scenarios: o.Scenarios, Warm: o.Warm.String(), Window: o.Window.String(), SearchWindow: o.SearchWindow.String(), MaxUsers: o.MaxUsers},
		}
	}
	bd := rec.Browse
	if bd.Attempts == nil {
		bd.Attempts = map[string][]Attempt{}
	}
	if bd.Common == nil {
		bd.Common = map[string]int{}
	}
	if bd.TraceCheck == nil {
		bd.TraceCheck = map[string]*TraceCheck{}
	}
	if o.From != nil {
		backfillAttempts(bd)
	}
	only := func(key string) bool { return len(o.Only) == 0 || slices.Contains(o.Only, key) }
	fmt.Fprintf(o.Log, "placement: %s\n", placement.Note)
	for _, sc := range o.Scenarios {
		sets, common, err := loadSets(o.TraceDir, specs, sc.Mean)
		if err != nil {
			return nil, err
		}
		for _, set := range sets {
			bd.Traces[set.file] = set.count
			if set.drop > 0 {
				bd.Dropped[set.file] = set.drop
			}
		}
		bd.Common[fmt.Sprintf("mean %d", sc.Mean)] = common
		bd.Plans = len(sets[specs[0].Name].plans)
		newReplayer := func(s Spec, server *Server) *replayer {
			r := &replayer{base: server.Base, write: s.Write, create: s.Create, traces: sets[s.Name].traces, plans: sets[s.Name].plans, sc: sc, seed: o.Seed, cpu: server.CPUSeconds, rss: server.PeakRSS}
			if s.Framework == "tifa" {
				r.buildID = buildID
			}
			return r
		}
		start := func(s Spec) (*Server, *replayer, error) {
			server, err := s.Launch(o.ServerCPUs)
			if err != nil {
				return nil, nil, err
			}
			if err := server.WaitListening(ctx); err != nil {
				server.Stop()
				return nil, nil, err
			}
			return server, newReplayer(s, server), nil
		}
		var active []Spec
		for _, s := range specs {
			key := sc.Name + "/" + s.Name
			if !only(key) {
				continue
			}
			if o.From != nil {
				// A rerun: the curve and the chosen step of the run it continues are kept. The rounds
				// of that run are what gets checked again, and they stay as rejected if they fail.
				if len(bd.Curves[key]) == 0 {
					return nil, fmt.Errorf("%s: the record to continue has no curve for it", key)
				}
				bd.Runs, bd.Rejected = moveRuns(bd.Runs, bd.Rejected, sc.Name, s.Name)
				active = append(active, s)
				continue
			}
			server, r, err := start(s)
			if err != nil {
				return nil, err
			}
			if err := r.verifyWrites(ctx); err != nil {
				server.Stop()
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			curve, chosen, err := r.search(ctx, o)
			server.Stop()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			bd.Curves[key] = curve
			bd.Chosen[key] = chosen
			fmt.Fprintf(o.Log, "%s: chose %d users\n", key, chosen)
			active = append(active, s)
		}
		// The rounds are checked against the rule of the search. A framework with a failing round steps
		// down to the next lower step that passes and all its rounds run again, with fresh servers.
		for attempt := 0; len(active) > 0; attempt++ {
			results := map[string][]BrowseRun{}
			for round := 1; round <= o.Rounds; round++ {
				for _, s := range Rotate(active, round-1) {
					key := sc.Name + "/" + s.Name
					if err := waitIdle(ctx, o.Log, o.ServerCPUs+","+o.LoadCPUs); err != nil {
						return nil, err
					}
					server, r, err := start(s)
					if err != nil {
						return nil, err
					}
					res, err := r.RunLoad(ctx, bd.Chosen[key], o.Warm, o.Window)
					if err == nil && round == o.Rounds {
						err = r.verifyWrites(ctx)
					}
					server.Stop()
					if err != nil {
						return nil, fmt.Errorf("%s round %d: %w", key, round, err)
					}
					results[key] = append(results[key], BrowseRun{Scenario: sc.Name, Spec: s.Name, Framework: s.Framework, Label: s.Label, Round: round, Source: o.Source, Load: res})
					fmt.Fprintf(o.Log, "round %d %-9s %-12s %2d users %7.2f visits/s %6.1f ms CPU/visit, %d errors %s\n", round, sc.Name, s.Name, res.Users, float64(res.Visits)/res.Seconds, 1000*res.CPU/max(1, float64(res.Visits)), res.Errors, res.FirstErr)
				}
			}
			var again []Spec
			for _, s := range active {
				key := sc.Name + "/" + s.Name
				users := bd.Chosen[key]
				reason := RoundsFail(results[key])
				bd.Attempts[key] = append(bd.Attempts[key], Attempt{Users: users, Failed: reason != "", Reason: reason, Source: o.Source})
				if reason == "" {
					bd.Runs = append(bd.Runs, results[key]...)
					continue
				}
				bd.Rejected = append(bd.Rejected, results[key]...)
				lower := StepDown(bd.Curves[key], users)
				if lower == 0 {
					return nil, fmt.Errorf("%s: the rounds at %d users fail (%s) and no lower step passes", key, users, reason)
				}
				fmt.Fprintf(o.Log, "%s: the rounds at %d users fail (%s), stepping down to %d\n", key, users, reason, lower)
				bd.Chosen[key] = lower
				again = append(again, s)
			}
			active = again
		}
	}
	if o.Check {
		// One round of each framework at its users of the baseline, to count what the servers answered.
		sc := BrowseScenarios[0]
		for _, s := range Rotate(specs, 0) {
			key := sc.Name + "/" + s.Name
			if bd.Chosen[key] == 0 {
				continue
			}
			checkSets, _, err := loadSets(o.TraceDir, specs, sc.Mean)
			if err != nil {
				return nil, err
			}
			set := checkSets[s.Name]
			server, err := s.Launch(o.ServerCPUs)
			if err != nil {
				return nil, err
			}
			if err := server.WaitListening(ctx); err != nil {
				server.Stop()
				return nil, err
			}
			r := &replayer{base: server.Base, write: s.Write, create: s.Create, traces: set.traces, plans: set.plans, sc: sc, seed: o.Seed, cpu: server.CPUSeconds, rss: server.PeakRSS}
			if s.Framework == "tifa" {
				r.buildID = buildID
			}
			res, err := r.RunLoad(ctx, bd.Chosen[key], o.Warm, o.Window)
			server.Stop()
			if err != nil {
				return nil, err
			}
			bd.TraceCheck[s.Name] = NewTraceCheck(s.Label, sc.Name, res)
			fmt.Fprintf(o.Log, "check %-12s %s\n", s.Name, bd.TraceCheck[s.Name].Summary())
		}
	}
	return rec, nil
}

// search raises the users 1, 2, 4, ... until a step fails, or visits/s rose by under 3% in two steps
// in a row (PlateauAfter: a fall counts as a rise under 3%), or o.MaxUsers is reached. It returns the
// whole curve and the step the rounds run at (ChooseStep).
func (r *replayer) search(ctx context.Context, o BrowseOptions) ([]CurvePoint, int, error) {
	var curve []CurvePoint
	for users := 1; users <= max(o.MaxUsers, 1); users *= 2 {
		res, err := r.RunLoad(ctx, users, o.SearchWarm, o.SearchWindow)
		if err != nil {
			return nil, 0, err
		}
		p := CurvePoint{Users: users, VisitsPerS: float64(res.Visits) / res.Seconds, Errors: res.Errors, DocP99: res.Types["doc"].P99, NavP99: res.Types["nav"].P99, Visits: res.Visits, OpenOldest: res.OpenOldestMs, Orders: res.Orders}
		p.Why = p.why()
		p.Pass = p.Why == ""
		curve = append(curve, p)
		fmt.Fprintf(o.Log, "  %2d users: %7.2f visits/s, %d errors, full load P99 %.0f ms, navigation P99 %.0f ms, %d orders in the shops %s\n", users, p.VisitsPerS, p.Errors, p.DocP99, p.NavP99, res.Orders["studio"]+res.Orders["weekend"], res.FirstErr)
		if !p.Pass || PlateauAfter(curve) {
			break
		}
	}
	chosen := ChooseStep(curve)
	if chosen == 0 {
		return curve, 0, fmt.Errorf("not even one user passes: %+v", curve)
	}
	return curve, chosen, nil
}

// PlateauAfter says whether the last two steps of the curve each brought less than 3% more visits/s
// than the best step before them. A fall is no more than that, so one dip does not end the search.
func PlateauAfter(curve []CurvePoint) bool {
	if len(curve) < 3 {
		return false
	}
	best := 0.0
	flat := 0
	for i, p := range curve {
		if i > 0 && p.VisitsPerS < best*1.03 {
			flat++
		} else {
			flat = 0
		}
		best = max(best, p.VisitsPerS)
	}
	return flat >= 2
}

// ChooseStep is the passing step with the most visits/s, the one with fewer users when two are
// equal. It is 0 when no step passes.
func ChooseStep(curve []CurvePoint) int {
	chosen, best := 0, 0.0
	for _, p := range curve {
		if p.Pass && p.VisitsPerS > best {
			chosen, best = p.Users, p.VisitsPerS
		}
	}
	return chosen
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

// verifyWrites checks, before the search and after the last round, that what the writes of the
// scenario change is what the server shows next: in the document of a page and in the data a client
// navigation fetches for it (as the browser asked for it in a trace). It uses addresses and ids of
// its own, so the server is not what it was.
func (r *replayer) verifyWrites(ctx context.Context) error {
	if r.sc.mix() == MixRename {
		return r.verifyRename(ctx)
	}
	return r.verifyOffice(ctx)
}

// get reads a page or the navigation data of a page.
func (r *replayer) get(ctx context.Context, url string, headers map[string]string) (string, error) {
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
	res, err := http.DefaultClient.Do(req)
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
func (r *replayer) nav(ctx context.Context, page string) (data string, ok bool, err error) {
	q, ok := r.navRequest(page)
	if !ok {
		return "", false, nil
	}
	if r.buildID != "" {
		q.URL = tifaBuild.ReplaceAllString(q.URL, "_tifa="+r.buildID+".")
	}
	data, err = r.get(ctx, q.URL, q.Headers)
	return data, true, err
}

// shows checks that the document of a page, and the navigation data of it when a trace has them,
// hold every text.
func (r *replayer) shows(ctx context.Context, what, page string, texts ...string) error {
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
func (r *replayer) verifyOffice(ctx context.Context) error {
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

// verifyRename writes to each writable order, both kinds of the rename mix (the address alone, then
// the address and the name), and checks that the document of the order shows it and that so does
// the data a client navigation fetches for it.
func (r *replayer) verifyRename(ctx context.Context) error {
	get := func(url string, headers map[string]string) (string, error) { return r.get(ctx, url, headers) }
	for _, fan := range []bool{false, true} {
		for _, w := range WritableOrders {
			n := int(r.writes.Add(1))
			st := WriteStep(r.write, w, n, fan)
			if err := Send(ctx, r.base, st); err != nil {
				return err
			}
			digits := fmt.Sprintf("%08d", n)
			url := "/projects/" + w.Project + "/orders/" + w.Order
			doc, err := get(url, nil)
			if err != nil {
				return err
			}
			if !strings.Contains(doc, digits) || fan && !strings.Contains(doc, st.Write.Customer) {
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
	}
	return nil
}

// navRequest finds, in the traces, the data request of a click to a page.
func (r *replayer) navRequest(page string) (TraceReq, bool) {
	for id := 0; id < len(r.plans); id++ {
		t, ok := r.traces[id]
		if !ok {
			continue
		}
		for _, s := range t.Steps {
			if s.Kind != StepClick || s.Page != page {
				continue
			}
			for _, q := range s.Reqs {
				if q.Class == "nav" && q.Method == "GET" && !strings.Contains(q.URL, "__manifest") {
					return q, true
				}
			}
		}
	}
	return TraceReq{}, false
}

// Attempt is the rounds of a framework at one step.
type Attempt struct {
	Users  int    `json:"users"`
	Failed bool   `json:"failed"`
	Reason string `json:"reason,omitempty"`
	Source string `json:"source,omitempty"`
}

// RoundsFail applies the rule of the search to the rounds of one framework at one step and says
// what failed ("" when every round passes): an error, or the P99 of a full load or of a whole
// navigation over a second.
func RoundsFail(runs []BrowseRun) string {
	var errs int
	var doc, nav, open float64
	var none int
	for _, r := range runs {
		errs += r.Load.Errors
		if r.Load.Visits == 0 {
			none++
		}
		open = max(open, r.Load.OpenOldestMs)
		doc = max(doc, r.Load.Types["doc"].P99)
		nav = max(nav, r.Load.Types["nav"].P99)
	}
	var out []string
	if none > 0 {
		out = append(out, fmt.Sprintf("%d rounds completed no visit", none))
	}
	if open > 1000 {
		out = append(out, fmt.Sprintf("a request had waited up to %.0f ms at the end of a window", open))
	}
	if errs > 0 {
		out = append(out, fmt.Sprintf("%d errors", errs))
	}
	if doc > 1000 {
		out = append(out, fmt.Sprintf("full load P99 up to %.0f ms", doc))
	}
	if nav > 1000 {
		out = append(out, fmt.Sprintf("navigation P99 up to %.0f ms", nav))
	}
	return strings.Join(out, ", ")
}

// StepDown is the highest step of the curve below users that passed the search, 0 for none.
func StepDown(curve []CurvePoint, users int) int {
	lower := 0
	for _, p := range curve {
		if p.Pass && p.Users < users {
			lower = max(lower, p.Users)
		}
	}
	return lower
}

// moveRuns takes the runs of a scenario and spec out of runs and puts them with the rejected ones,
// as the attempt that is run again.
func moveRuns(runs, rejected []BrowseRun, scenario, spec string) (keep, rej []BrowseRun) {
	for _, r := range runs {
		if r.Scenario == scenario && r.Spec == spec {
			rejected = append(rejected, r)
		} else {
			keep = append(keep, r)
		}
	}
	return keep, rejected
}

// CurveEnd says why the search stopped and which step the rounds started at.
func CurveEnd(curve []CurvePoint, maxUsers int) string {
	if len(curve) == 0 {
		return ""
	}
	last := curve[len(curve)-1]
	if maxUsers == 0 {
		maxUsers = 128
	}
	var why string
	switch {
	case !last.Pass:
		why = fmt.Sprintf("the step at %d users fails (%s)", last.Users, last.Why)
	case PlateauAfter(curve):
		why = "visits/s rose under 3% in two steps in a row"
	case last.Users >= maxUsers:
		why = fmt.Sprintf("cap reached at %d users", last.Users)
	default:
		why = "ended"
	}
	chosen := ChooseStep(curve)
	var at float64
	for _, p := range curve {
		if p.Users == chosen {
			at = p.VisitsPerS
		}
	}
	return fmt.Sprintf("stopped: %s. Chosen: %d users, the passing step with the most visits/s (%.0f)", why, chosen, at)
}

// backfillAttempts gives the runs and the keys of a record that was made before attempts were kept
// a source and an attempt each, from the rule applied to their rounds.
func backfillAttempts(bd *BrowseData) {
	by := map[string][]BrowseRun{}
	for i := range bd.Runs {
		if bd.Runs[i].Source == "" {
			bd.Runs[i].Source = "run 1"
		}
		key := bd.Runs[i].Scenario + "/" + bd.Runs[i].Spec
		by[key] = append(by[key], bd.Runs[i])
	}
	for key, runs := range by {
		if len(bd.Attempts[key]) > 0 {
			continue
		}
		reason := RoundsFail(runs)
		bd.Attempts[key] = []Attempt{{Users: runs[0].Load.Users, Failed: reason != "", Reason: reason, Source: "run 1"}}
	}
}

// MergeBrowse puts the runs of some specs from part into base, replacing what base held for them:
// their curves, chosen steps, attempts, rounds and trace checks. It is for rerunning a framework
// without running the others again, and fails if the plans replayed are not the same in both.
func MergeBrowse(base, part *Record, specs []string) error {
	b, p := base.Browse, part.Browse
	if b == nil || p == nil {
		return fmt.Errorf("both records need a browse part")
	}
	for k, n := range p.Common {
		if b.Common[k] != n {
			return fmt.Errorf("%s: the new run replays %d plans, the record it goes into %d", k, n, b.Common[k])
		}
	}
	in := func(spec string) bool { return slices.Contains(specs, spec) }
	keySpec := func(key string) string { _, spec, _ := strings.Cut(key, "/"); return spec }
	for key := range b.Curves {
		if in(keySpec(key)) {
			delete(b.Curves, key)
			delete(b.Chosen, key)
			delete(b.Attempts, key)
		}
	}
	for key, v := range p.Curves {
		b.Curves[key], b.Chosen[key] = v, p.Chosen[key]
		if a := p.Attempts[key]; len(a) > 0 {
			if b.Attempts == nil {
				b.Attempts = map[string][]Attempt{}
			}
			b.Attempts[key] = a
		}
	}
	keep := func(runs []BrowseRun) (out []BrowseRun) {
		for _, r := range runs {
			if !in(r.Spec) {
				out = append(out, r)
			}
		}
		return out
	}
	b.Runs = append(keep(b.Runs), p.Runs...)
	b.Rejected = append(keep(b.Rejected), p.Rejected...)
	for spec, c := range p.TraceCheck {
		if b.TraceCheck == nil {
			b.TraceCheck = map[string]*TraceCheck{}
		}
		b.TraceCheck[spec] = c
	}
	for f, n := range p.Traces {
		b.Traces[f] = n
	}
	for f, n := range p.Dropped {
		b.Dropped[f] = n
	}
	return nil
}
