package compare

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func closedRound(fw, sc string, sessions, round int, views int, seconds float64, pass bool) Round {
	return Round{Framework: fw, Scenario: sc, Model: "closed", Sessions: sessions, Round: round, Blocks: 4, Visits: views / 4, PageViews: views, FullLoads: views / 4, Clicks: views * 3 / 4,
		Writes: views / 19, WriteShare: 0.05, ClickShare: 0.75, ViewsPerVisit: 4, Seconds: seconds, DrainSeconds: 1.5, CPUSeconds: seconds / 2, PeakRSS: 100, Requests: views * 2, Bytes: int64(views) * 4096,
		Responses: map[string]int{"doc 200 text/html": views / 4, "nav 200 application/json": views * 3 / 4, "write 200 application/json": views / 19},
		Renders:   map[string]int{"vm": 3, "memo": views/4 - 3},
		Types:     map[string]TypeStat{"load": {N: 100, P50: 4, P99: 30}, "click": {N: 300, P50: 2, P99: 15}, "write": {N: 20, P50: 5, P99: 9}},
		Valid:     true, Pass: pass, Why: map[bool]string{true: "", false: "click P99 1200 ms over 1000 ms"}[pass]}
}

func TestBrowseReportShowsFailedCellsAndTheLatencyRule(t *testing.T) {
	mix := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{
		"shop/nextjs": {Rate: 16, Done: true}},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Config: map[string]string{"sessions": "32"}}
	for round := 1; round <= 3; round++ {
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 32, round, 4000, 10, true), closedRound("nextjs", mix.Name, 32, round, 400, 10, round != 2))
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 8, round, 3000, 10, true), closedRound("nextjs", mix.Name, 8, round, 380, 10, true))
		l := closedRound("tifa", mix.Name, 0, round, 1000, 100, true)
		l.Framework, l.Model, l.Rate = "tifa", "open", 8
		r.Latency = append(r.Latency, l)
	}
	md := r.Markdown()
	for _, want := range []string{
		"### the shop: shoppers and staff",
		"| | 8 sessions | 32 sessions |",
		"| Tifa | 300.0 | 400.0 |",
		"| Next.js | 38.0 | failed (click P99 1200 ms over 1000 ms) |", // one of three rounds failed: the cell failed
		"#### The arrival rate Next.js sustains",
		"| Next.js | 16.00 | 42 |",
		"#### Latency at the same arrival rate, half of Next.js's confirmed rate",
		"not half of its own capacity",
		"The writes are requests inserted into the recorded visits",
		"the refresh after a write is not replayed",
		"Every server gets 8.00 visits/s",
		"What one visit costs the server",
		"What the server did a second:",
		"| Tifa | 100.0 | 300.0 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
}

// The latency table gives every round a server played at the rate, the failed ones too: a server
// with a failed round fails the rule, and its errors and tail latency are in its row.
func TestLatencyTableKeepsFailedRoundsAndSaysSo(t *testing.T) {
	mix := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"},
		Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}}
	for round := 1; round <= 3; round++ {
		a := closedRound("nextjs", mix.Name, 0, round, 1000, 100, round != 2)
		a.Model = "open"
		if round == 2 {
			a.TimedOut, a.Failures = true, 20
			a.Why = "the workload did not finish in time: a backlog was left"
			a.Types["load"] = TypeStat{N: 5, P50: 3000, P99: 3000}
		}
		r.Latency = append(r.Latency, a)
		b := closedRound("tifa", mix.Name, 0, round, 1000, 100, false)
		b.Model, b.Framework = "open", "tifa"
		r.Latency = append(r.Latency, b)
	}
	md := r.Markdown()
	for _, want := range []string{
		"| Next.js | 4.0 (4.0 to 3000.0) / 30 (30 to 3000) (205) |", "| 20 |",
		"failed the rule in 1 of 3 rounds: round 2 failed: the workload did not finish in time",
		// A server that fails at the rate in every round is a result, with its numbers.
		"| Tifa | 4.0 / 30 (300) | 2.0 / 15 (900) | 5.0 / 9 (60) | failed the rule in 3 of 3 rounds: round 1 failed: click P99 1200 ms over 1000 ms", "| Tifa | 0 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
}

// TestBrowseStampBindsWhatItShould checks that the lines of the stamps that browse.sh prints name the
// sources, the executables that run, the replayer and the trace files: a stamp that leaves one out
// lets a stale trace or result be used.
func TestBrowseStampBindsWhatItShould(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("bash", append([]string{filepath.Join(env.Root, "test", "compare", "browse.sh")}, args...)...)
		cmd.Dir = env.Root
		cmd.Env = append(os.Environ(), "BROWSE_OUT="+out)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("browse.sh %s: %v\n%s", args, err, data)
		}
		return string(data)
	}
	trace := run("stamp")
	for _, want := range []string{"tree js ", "tree internal ", "tree server ", "tree cmd ", "tree root-go ", "tree test/compare/recorder ", "tree test/compare/plan.go ", "tree test/compare/mix.go ", "tree test/compare/store_plan.go ", "tree test/compare/store_links.json ", "tree examples/shop ", "tree test/compare/nextjs ", "tree test/compare/start ", "tree test/compare/reactrouter ", "playwright ", "site shop\n", "plans none\n"} {
		if !strings.Contains(trace, want) {
			t.Errorf("the trace stamp lacks %q:\n%s", want, trace)
		}
	}
	// The stamp is bound to the plans of the site.
	os.MkdirAll(filepath.Join(out, "traces"), 0o755)
	os.WriteFile(filepath.Join(out, "traces", "plans.json"), []byte("{}"), 0o644)
	if strings.Contains(run("stamp"), "plans none") {
		t.Error("the stamp is not bound to the plans")
	}
	results := run("runstamp")
	for _, want := range []string{"traces ", "trace-files ", "replayer ", "replayer-cmd ", "shop-binary ", "next-output ", "start-output ", "reactrouter-output ", "tifa-build ", "settings ", "go ", "node "} {
		if !strings.Contains(results, want) {
			t.Errorf("the result stamp lacks %q:\n%s", want, results)
		}
	}
	// There is one scenario: a command line that still picks one is refused, not run on the shop.
	cmd := exec.Command("bash", filepath.Join(env.Root, "test", "compare", "browse.sh"), "stamp")
	cmd.Dir, cmd.Env = env.Root, append(os.Environ(), "BROWSE_OUT="+out, "SCENARIOS=shop-staff-x2")
	if data, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(data), "SCENARIOS is gone") {
		t.Errorf("SCENARIOS is taken: %v\n%s", err, data)
	}
}

// Every scenario gets the tables it has, with its own servers: the closed tables for a scenario with
// throughput, the search and the latency for one with latency, each with its own rate. The writes of
// a round are given for the rounds a scenario has.
func TestBrowseReportHasTheTablesOfEveryScenario(t *testing.T) {
	scs := BrowseScenarios
	r := &BrowseResult{Scenarios: scs, Servers: map[string][]string{}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "32"},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{}}
	for i, sc := range scs {
		servers := BrowseFrameworks
		r.Servers[sc.Name] = servers
		if sc.Latency {
			r.LatencyRate[sc.Name], r.LatencyBase[sc.Name] = float64(i+2), "nextjs"
			r.Sustain[sustainKey(sc.Name, "nextjs")] = &Sustain{Rate: float64(2 * (i + 2)), Done: true}
		}
		for _, f := range servers {
			x := closedRound(f, sc.Name, 32, 1, 4000, 10, true)
			x.WriteKinds = map[string]int{WriteShip: 30, WriteAdd: 50, WriteCheckout: 20}
			x.Orders = map[string]int{"studio": 82, "weekend": 12}
			if sc.Throughput {
				r.Closed = append(r.Closed, x)
			}
			if sc.Latency {
				l := x
				l.Model, l.Rate, l.Sessions = "open", float64(i+2), 0
				r.Latency = append(r.Latency, l)
			}
		}
	}
	md := r.Markdown()
	sections := strings.Split(md, "\n### ")
	for i, sc := range scs {
		// The results of the scenario, and its diagnostics in the appendix.
		var sec string
		for _, s := range sections {
			if strings.HasPrefix(s, sc.Title+"\n") || strings.HasPrefix(s, sc.Title+": what is behind") {
				sec += s
			}
		}
		if sec == "" {
			t.Fatalf("no section for %s:\n%s", sc.Title, md)
		}
		closed := []string{"#### Page views a second at each number of concurrent sessions", "#### 32 concurrent sessions"}
		open := []string{"#### The arrival rate Next.js sustains", "#### Latency at the same arrival rate", fmt.Sprintf("Every server gets %.2f visits/s", float64(i+2))}
		for _, want := range append(closed, open...) {
			has := strings.Contains(sec, want)
			if slices.Contains(closed, want) && has != sc.Throughput || slices.Contains(open, want) && has != sc.Latency {
				t.Errorf("%s has %q: %v", sc.Name, want, has)
			}
		}
		if !sc.Latency && !strings.Contains(sec, "This scenario has no open run") {
			t.Errorf("%s does not say it has no open run:\n%s", sc.Name, sec)
		}
		if !sc.Throughput && !strings.Contains(sec, "This scenario has no throughput table") {
			t.Errorf("%s does not say it has no throughput table:\n%s", sc.Name, sec)
		}
		for _, f := range r.Servers[sc.Name] {
			if !strings.Contains(sec, "| "+BrowseLabel(f)+" |") {
				t.Errorf("%s has no row for %s", sc.Name, f)
			}
		}
		if !strings.Contains(sec, "| add to cart | check out | edit a product | ship |") || !strings.Contains(sec, "| 82 / 12 |") || !strings.Contains(sec, "| 50 | 20 | 0 | 30 | ") {
			t.Errorf("%s lacks the table of writes:\n%s", sc.Name, sec)
		}
	}
	for _, want := range []string{"The comparison plays the shop as one site", "\n### The mix\n", "| visits made by the staff in the back office | 2% | assumption |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	for _, f := range MixFacts() {
		if !strings.Contains(md, "| "+f.What+" | "+f.Value+" | "+f.Source+" |") {
			t.Errorf("the report lacks the mix fact %q", f.What)
		}
	}
	if !strings.Contains(md, "| shopper visits that add to the cart | 6.6% |") {
		t.Error("the share of adds is not written as 6.6%")
	}
}

func TestReportTellsABrowseResultFromARecord(t *testing.T) {
	data, err := (&BrowseResult{}).json()
	if err != nil {
		t.Fatal(err)
	}
	if !IsBrowseResult(data) {
		t.Error("a browse result is not one")
	}
	if IsBrowseResult([]byte(`{"date":"x","runs":[]}`)) {
		t.Error("a record of measure is a browse result")
	}
}

// For Tifa the report splits the render memo hits of full loads by kind of page, in every closed
// run and in the read-only control of admin-w5: a diagnostic of what the writes do to the cache.
func TestBrowseReportSplitsTheMemoOfFullLoadsByPage(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "32"}}
	x := closedRound("tifa", sc.Name, 32, 1, 4000, 10, true)
	x.PageRenders = map[string]map[string]int{"list": {"memo": 55, "vm": 45}, "stats": {"memo": 11, "vm": 9}, "order": {"memo": 1, "vm": 99}, "cart": {"memo": 3, "vm": 7}}
	x.PrivateHits = 3
	x.Memo = &MemoRound{Start: MemoCounts{Hits: 10}, End: MemoCounts{Hits: 260}} // 250 hits in 1000 visits
	n := closedRound("nextjs", sc.Name, 32, 1, 400, 10, true)
	c := closedRound("tifa", sc.Name, 32, 1, 8000, 10, true)
	c.Model, c.Writes, c.WriteShare = "control", 0, 0
	c.PageRenders = map[string]map[string]int{"list": {"memo": 98, "vm": 2}, "stats": {"memo": 19, "vm": 1}, "order": {"memo": 97, "vm": 3}}
	r.Closed, r.Control = []Round{x, n}, []Round{c}
	md := r.Markdown()
	for _, want := range []string{
		"render memo hits by kind of page",
		"| | cart | list | order | stats | private pages from the memo |",
		"| Tifa | 30.0% of 10 | 55.0% of 100 | 1.0% of 100 | 55.0% of 20 | 3 of 10 |",
		"#### Read-only control",
		"| Tifa, no writes | 800.0 | - | 98.0% of 100 | 97.0% of 100 | 95.0% of 20 |",
		"| Tifa, with writes | 400.0 | 30.0% of 10 | 55.0% of 100 | 1.0% of 100 | 55.0% of 20 |",
		"| renders | cache hits | shared renders | memo hits Tifa counted |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| Next.js | 55.0%") {
		t.Error("the split is given for a server that is not Tifa")
	}
}

// The throughput table types each server's result and gives the sessions it was measured at, its
// page views a second, the active people they stand for and its server cores. A server whose next
// step failed reports the step below, never the failed one, and the table of every number of
// sessions shows the failed cell as failed.
func TestBrowseReportTypesTheThroughputOfEachServer(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "start"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{},
		Config: map[string]string{"sessions": "8", "rounds": "3"},
		Throughput: map[string]*Throughput{
			"shop/tifa":   {Result: ThroughputCoreUsed, Sessions: 13, Why: "used 0.96 of 1 cpus at 13 sessions", Steps: []int{8, 10, 13}},
			"shop/nextjs": {Result: ThroughputHighestPassing, Sessions: 8, Why: "failed at 10 sessions: click P99 1200 ms over 1000 ms", Steps: []int{8, 10}},
			"shop/start":  {Result: ThroughputNone, Why: "failed at 8, 6 and 4 sessions: click P99 1200 ms over 1000 ms", Steps: []int{8, 6, 4}},
		}}
	for round, d := range []int{-10, 0, 10} {
		r.Closed = append(r.Closed, closedRound("tifa", sc.Name, 8, round+1, 4000+d, 10, true), closedRound("tifa", sc.Name, 10, round+1, 4500+d, 10, true), closedRound("tifa", sc.Name, 13, round+1, 5000+d, 10, true),
			closedRound("nextjs", sc.Name, 8, round+1, 400+d, 10, true))
	}
	r.Closed = append(r.Closed, closedRound("nextjs", sc.Name, 10, 1, 9000, 10, false))
	for _, n := range []int{8, 6, 4} {
		r.Closed = append(r.Closed, closedRound("start", sc.Name, n, 1, 300, 10, false))
	}
	md := r.Markdown()
	for _, want := range []string{
		"#### Throughput",
		"| | result | sessions | page views/s | active people at one page per 5 to 10 s | full load P50 / P99 ms (n) |",
		"| | server cores | why the steps stopped | sessions played |",
		"| Tifa | core used | 13 | 500.0 (499.0 to 501.0) | 2500 to 5000 |", "| Tifa | 0.50 |",
		"| Next.js | highest passing step | 8 | 40.0 (39.0 to 41.0) | 200 to 400 |", "| Next.js | 0.50 |",
		"| TanStack Start | no passing step | - | - |",
		"| | 4 sessions | 6 sessions | 8 sessions | 10 sessions | 13 sessions |",
		"| Next.js | - | - | 40.0 (39.0 to 41.0) | failed (click P99 1200 ms over 1000 ms) | - |",
		"| TanStack Start | failed (click P99 1200 ms over 1000 ms) | failed (click P99 1200 ms over 1000 ms) | failed (click P99 1200 ms over 1000 ms) | - | - |",
		"#### 8 concurrent sessions",
		"#### 13 concurrent sessions",
		"| server cores | client cores |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "900.0") || strings.Contains(md, "#### 10 concurrent sessions") {
		t.Error("the report counts a failed round, or gives the detail of a step that is no result")
	}
}

// A round with no samples of a kind of action has no latency of it: it is left out of the cell, and
// not taken as 0 ms.
func TestTypeCellLeavesOutRoundsWithNoSamples(t *testing.T) {
	rs := []Round{
		{Types: map[string]TypeStat{"load": {N: 10, P50: 5, P99: 9}}},
		{Types: map[string]TypeStat{"load": {N: 10, P50: 5, P99: 9}, "write": {N: 0}}},
		{Types: map[string]TypeStat{"load": {N: 10, P50: 5, P99: 9}, "write": {N: 4, P50: 700, P99: 1500}}},
	}
	if got, want := typeCell(rs, "write"), "700.0 / 1500 (4)"; got != want {
		t.Errorf("write cell %q, want %q", got, want)
	}
	if got := typeCell(rs, "click"); got != "-" {
		t.Errorf("click cell %q, want -", got)
	}
}

func mustScenario(name string) BrowseScenario {
	sc, ok := BrowseScenarioByName(name)
	if !ok {
		panic(name)
	}
	return sc
}

// A kind of page that the render memo never or seldom answers in the read-only control, where
// nothing changes, is a red flag: something in its data changes on every request (a clock, a
// random value). It is flagged above the control table, and a kind with few answers is not.
func TestTheReportFlagsAPageTheCacheMissesWithNoWrites(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	c := closedRound("tifa", sc.Name, 8, 1, 8000, 10, true)
	c.Model, c.Writes = "control", 0
	c.PageRenders = map[string]map[string]int{"list": {"memo": 98, "vm": 2}, "order": {"vm": 300}, "stats": {"memo": 30, "vm": 70}, "home": {"vm": 5}}
	l := closedRound("tifa", sc.Name, 0, 1, 1000, 100, true)
	l.Model, l.PageRenders = "open", map[string]map[string]int{"list": {"memo": 50, "vm": 50}}
	r.Control, r.Latency = []Round{c}, []Round{l}
	md := r.Markdown()
	for _, want := range []string{
		"**Red flag**: with no writes the render memo answered order 0.0% of 300, stats 30.0% of 100.",
		"| Tifa, no writes | 800.0 |",
		"| Tifa, with writes, the latency rounds | 10.0 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "home 0.0%") {
		t.Error("a kind of page with 5 answers is flagged")
	}
	c.PageRenders = map[string]map[string]int{"list": {"memo": 98, "vm": 2}}
	r.Control = []Round{c}
	if strings.Contains(r.Markdown(), "Red flag") {
		t.Error("a control that hits is flagged")
	}
}

// The report claims only what the method measures: the half rate is a rate shared by every server,
// not a promise of answers free of queueing, the mix of a round is what it planned, the admin
// rounds grow their orders and memory (not a claim of heavier reads), and a page the control
// seldom hits is flagged to look into, with no cause named.
func TestTheReportClaimsOnlyWhatItMeasures(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"},
		Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}, LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}}
	c := closedRound("tifa", sc.Name, 8, 1, 8000, 10, true)
	c.Model, c.Writes, c.PageRenders = "control", 0, map[string]map[string]int{"order": {"vm": 300}}
	l := closedRound("tifa", sc.Name, 0, 1, 1000, 100, true)
	l.Model, l.Rate = "open", 8
	r.Control, r.Latency = []Round{c}, []Round{l}
	md := r.Markdown()
	for _, want := range []string{"every round plans the mix of its scenario", "and so does the memory that holds them", "Look into why: this table does not tell the cause."} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	for _, claim := range []string{"not how long requests wait in line", "get heavier the more orders", "since something in its data changes on every request", "with nothing changing, something in its data changes"} {
		if strings.Contains(md, claim) {
			t.Errorf("the report claims %q", claim)
		}
	}
}

// The report shows, per server and scenario, how the first valid round went in 10 second steps:
// Tifa's cache filling up and the JIT of the Node servers warming show as a change over the round.
func TestBrowseReportShowsTheSeriesOfEachServer(t *testing.T) {
	mix := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{},
		Config: map[string]string{"sessions": "32"}}
	steps := func(rates, p99 []float64) []SeriesStep {
		var out []SeriesStep
		for i := range rates {
			out = append(out, SeriesStep{From: 10 * i, PageViews: int(rates[i] * 10), PerSecond: rates[i], P99Ms: p99[i]})
		}
		return out
	}
	tifa := closedRound("tifa", mix.Name, 32, 1, 4000, 30, true)
	tifa.Series = steps([]float64{210, 380, 402.5}, []float64{240, 90, 61})
	node := closedRound("nextjs", mix.Name, 32, 1, 400, 30, true)
	node.Series = steps([]float64{30, 41, 44}, []float64{800, 300, 250})
	open := closedRound("nextjs", mix.Name, 0, 1, 400, 30, true)
	open.Model, open.Rate = "open", 8
	open.Series = steps([]float64{32, 32}, []float64{120, 100})
	r.Closed = append(r.Closed, tifa, node)
	r.Latency = append(r.Latency, open)
	md := r.Markdown()
	for _, want := range []string{
		"#### Page views a second and P99 in 10 second steps",
		"| Tifa | closed, 32 sessions, round 1 | 210.0 380.0 402.5 | 240 90 61 |",
		"| Next.js | closed, 32 sessions, round 1 | 30.0 41.0 44.0 | 800 300 250 |",
		"| Next.js | open, round 1 | 32.0 32.0 | 120 100 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
}

// The body of the report holds the results alone: for each scenario its throughput table and its
// latency table, then the mix with the source of every number. Every diagnostic is in the appendix
// after it, and each result table links to the diagnostics behind it.
func TestTheReportBodyHoldsTheResultsAndTheAppendixTheDiagnostics(t *testing.T) {
	scs := BrowseScenarios
	r := &BrowseResult{Scenarios: scs, Servers: map[string][]string{}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{}, Throughput: map[string]*Throughput{}}
	for _, sc := range scs {
		r.Servers[sc.Name] = []string{"tifa", "nextjs"}
		r.LatencyRate[sc.Name], r.LatencyBase[sc.Name] = 4, "nextjs"
		r.Sustain[sustainKey(sc.Name, "nextjs")] = &Sustain{Rate: 8, Done: true}
		for _, f := range r.Servers[sc.Name] {
			r.Throughput[sustainKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
			x := closedRound(f, sc.Name, 8, 1, 4000, 10, true)
			x.WriteKinds = map[string]int{WriteAdd: 5}
			x.PageRenders = map[string]map[string]int{"product": {"vm": 3, "memo": 7}}
			x.Series = []SeriesStep{{From: 0, PageViews: 4000, PerSecond: 400, P99Ms: 30}}
			r.Closed = append(r.Closed, x)
			l := x
			l.Model, l.Rate, l.Sessions = "open", 4, 0
			r.Latency = append(r.Latency, l)
		}
	}
	c := closedRound("tifa", scs[0].Name, 8, 1, 8000, 10, true)
	c.Model, c.Writes, c.PageRenders = "control", 0, map[string]map[string]int{"product": {"vm": 1, "memo": 99}}
	r.Control = []Round{c}
	md := r.Markdown()
	body, appendix, ok := strings.Cut(md, "\n## Appendix\n")
	if !ok {
		t.Fatalf("the report has no appendix:\n%s", md)
	}
	// The tables of the body have the columns of the results alone: what each server reached and its
	// latency. The server cores, why the steps stopped, the sessions played, the failures, the
	// processor time and how steps and rounds are judged are in the appendix.
	var heads []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "| |") {
			heads = append(heads, l)
		}
	}
	var want []string
	for range scs {
		want = append(want, "| | result | sessions | page views/s | active people at one page per 5 to 10 s | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) |",
			"| | full loads | clicks | writes | rounds |")
	}
	want = append(want, "| | value | source |")
	if !slices.Equal(heads, want) {
		t.Errorf("the tables of the body have the columns\n%s\nwant\n%s", strings.Join(heads, "\n"), strings.Join(want, "\n"))
	}
	for _, method := range []string{
		"| | server cores | why the steps stopped | sessions played |",
		"| | failures | visits failed / unfinished, of planned | server CPU ms/page view | not valid, run again |",
		"is stepped up about 25% a step",
		"A round in which the load generator used over 0.8 of its cpus is not valid",
		"A server that cannot hold that rate fails at it",
	} {
		if strings.Contains(body, method) || !strings.Contains(appendix, method) {
			t.Errorf("%q is not in the appendix alone", method)
		}
	}
	// The body: the four result tables in order, each with its link, then the mix.
	at := 0
	for _, sc := range scs {
		for _, part := range []struct{ heading, anchor string }{{"#### Throughput", "throughput"}, {"#### Latency at the same arrival rate", "latency"}} {
			i := strings.Index(body[at:], part.heading)
			if i < 0 {
				t.Fatalf("the body lacks the %s table of %s after its earlier tables:\n%s", part.anchor, sc.Name, body)
			}
			at += i + len(part.heading)
			link := fmt.Sprintf("](#diagnostics-%s-%s)", sc.Name, part.anchor)
			next := strings.Index(body[at:], "\n#### ")
			if next < 0 {
				next = len(body) - at
			}
			if !strings.Contains(body[at:at+next], link) {
				t.Errorf("the %s table of %s does not link to its diagnostics (%s)", part.anchor, sc.Name, link)
			}
			if !strings.Contains(appendix, fmt.Sprintf(`<a id="diagnostics-%s-%s"></a>`, sc.Name, part.anchor)) {
				t.Errorf("the appendix has no anchor for the %s diagnostics of %s", part.anchor, sc.Name)
			}
		}
	}
	if i := strings.Index(body, "\n### The mix\n"); i < at {
		t.Errorf("the mix is not after the result tables in the body")
	}
	for _, f := range MixFacts() {
		if !strings.Contains(body, "| "+f.What+" | "+f.Value+" | "+f.Source+" |") {
			t.Errorf("the body lacks the mix fact %q", f.What)
		}
	}
	// Every diagnostic is in the appendix and none in the body.
	for _, diag := range []string{
		"#### The arrival rate Next.js sustains",
		"#### Page views a second at each number of concurrent sessions",
		"What the server did a second:",
		"What one visit costs the server",
		"What the writes of a round were",
		"What the rounds planned and did",
		"the render memo hits by kind of page",
		"private pages from the memo",
		"#### Read-only control",
		"#### Page views a second and P99 in 10 second steps",
		"### Do the traces replay on this machine",
	} {
		if strings.Contains(body, diag) {
			t.Errorf("the body holds the diagnostic %q", diag)
		}
		if !strings.Contains(appendix, diag) {
			t.Errorf("the appendix lacks the diagnostic %q", diag)
		}
	}
}

// The cost of a visit gives, for Tifa, the hits its render memo counted itself over the round, per
// visit, from its metrics before and after the round.
func TestBrowseReportGivesTheHitsTifaCounted(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "32"}}
	x := closedRound("tifa", sc.Name, 32, 1, 4000, 10, true)
	x.Memo = &MemoRound{Start: MemoCounts{Hits: 10}, End: MemoCounts{Hits: 260}} // 250 hits in 1000 visits
	r.Closed = []Round{x, closedRound("nextjs", sc.Name, 32, 1, 400, 10, true)}
	md := r.Markdown()
	if !strings.Contains(md, "| memo hits Tifa counted |") || !slices.ContainsFunc(strings.Split(md, "\n"), func(l string) bool {
		return strings.HasPrefix(l, "| Tifa |") && strings.HasSuffix(l, " | 0.250 |")
	}) {
		t.Errorf("the report lacks the hits Tifa counted:\n%s", md)
	}
}

// The latency table of the body lists every server of the scenario, whatever its rounds were: one
// with no valid round has no valid result, one with fewer valid rounds than the run plays is not
// decided, one whose valid rounds all passed passed, and one with a valid round that failed failed.
func TestTheLatencyTableListsEveryServerWithWhatItsRoundsDecide(t *testing.T) {
	mix := mustScenario("shop")
	fws := []string{"tifa", "nextjs", "start", "reactrouter"}
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: fws}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Config: map[string]string{"rounds": "3"},
		Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}}
	open := func(fw string, round int, valid, pass bool) Round {
		x := closedRound(fw, mix.Name, 0, round, 1000, 100, pass)
		x.Model, x.Rate, x.Valid = "open", 8, valid
		if !valid {
			x.WhyInvalid = "the load generator started a visit 400 ms late"
		}
		return x
	}
	for round := 1; round <= 3; round++ {
		for try := 1; try <= 3; try++ {
			x := open("tifa", round, false, true) // every try of every round not valid
			x.Attempt = try
			r.Latency = append(r.Latency, x)
		}
		r.Latency = append(r.Latency, open("nextjs", round, true, true), open("reactrouter", round, true, round != 3))
		r.Latency = append(r.Latency, open("start", round, round == 1, true)) // one valid round of three
	}
	md := r.Markdown()
	body, _, _ := strings.Cut(md, "\n## Appendix\n")
	row := func(label string) string {
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, "| "+label+" |") && strings.Contains(l, "round") || strings.HasPrefix(l, "| "+label+" |") && strings.Contains(l, "no valid result") {
				return l
			}
		}
		return ""
	}
	for label, want := range map[string]string{
		"Tifa":           "| Tifa | - | - | - | no valid result: 0 of 3 rounds valid |",
		"TanStack Start": "not decided: 1 of 3 rounds valid, 1 of 1 passed |",
		"Next.js":        "3 of 3 rounds passed |",
		"React Router":   "failed the rule in 1 of 3 rounds: round 3 failed: click P99 1200 ms over 1000 ms |",
	} {
		if got := row(label); !strings.Contains(got, want) {
			t.Errorf("the latency row of %s is %q, want it to hold %q:\n%s", label, got, want, body)
		}
	}
}

// The appendix says how the writes work as the runner makes them: an edit is a PATCH of one field of
// a product, every ship ships an order placed for it before the clock, and a new visitor sees the
// demonstration cart, the same for every new visitor. What an earlier version did is not said.
func TestTheReportDescribesTheWritesAsTheyAreMade(t *testing.T) {
	for _, old := range []string{"no product edit yet", "changes the recipient of an order for now", "oldest order that has not shipped", "places one instead", "Every visit has data of its own", "differs from visit to visit"} {
		if strings.Contains(siteText, old) {
			t.Errorf("the report still says %q", old)
		}
	}
	for _, want := range []string{
		"an edit is one PATCH of one field of a product",
		"each ship ships an order of its own, placed for it before the clock",
		"the private data of every visit that has some (the cart of a returning visitor, a checkout) is drawn for that visit alone from the seed and the visit",
		"the time of an order is the clock of the server when it is placed",
		"a new visitor sees the demonstration cart, the same for every new visitor",
	} {
		if !strings.Contains(siteText, want) {
			t.Errorf("the report does not say %q", want)
		}
	}
}

// A capped round (Capped) played every visit prepared for it. The body marks a cell whose rounds
// include one and says under the table that their visits reached the cap of the prepared visits,
// without saying that they were short. The appendix lists every capped try of the scenario, at any
// number of sessions and valid or not, with how long it took against the window of the target.
func TestTheReportListsEveryCappedTry(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{},
		Config: map[string]string{"sessions": "8", "rounds": "3", "target": "1m15s"},
		Throughput: map[string]*Throughput{
			"shop/tifa":   {Result: ThroughputPlateau, Sessions: 10, Steps: []int{8, 10, 13}},
			"shop/nextjs": {Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}},
		}}
	for round := 1; round <= 3; round++ {
		at10 := closedRound("tifa", sc.Name, 10, round, 4000, 90, true)
		at10.Capped = round == 2 // in the window of the target: 60 to 120 s
		at13 := closedRound("tifa", sc.Name, 13, round, 4000, 40, true)
		at13.Capped = true // a step that is not the result, short
		r.Closed = append(r.Closed, closedRound("tifa", sc.Name, 8, round, 4000, 80, true), at10, at13, closedRound("nextjs", sc.Name, 8, round, 400, 80, true))
	}
	bad := closedRound("tifa", sc.Name, 13, 1, 4000, 40, true)
	bad.Capped, bad.Valid, bad.Attempt, bad.WhyInvalid = true, false, 2, "the load generator used 1.90 cores of 2"
	r.Closed = append(r.Closed, bad)
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	row := func(text, start string) string {
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, start) {
				return l
			}
		}
		return ""
	}
	if tifa := row(body, "| Tifa | plateau"); !strings.Contains(tifa, cappedMark) {
		t.Errorf("the body row of Tifa has no mark of its capped round: %q", tifa)
	}
	if next := row(body, "| Next.js | core used"); strings.Contains(next, cappedMark) {
		t.Errorf("the body marks Next.js, none of whose rounds was capped: %q", next)
	}
	note := row(body, cappedMark+" ")
	if !strings.Contains(note, "prepared") || strings.Contains(note, "shorter") {
		t.Errorf("the note under the table is %q: it must say that the visits reached the prepared ones, and not that the round was short", note)
	}
	// Every capped try: Tifa at 10 sessions (in the window), at 13 sessions three valid (short) and
	// one not valid.
	var tries []string
	for _, l := range strings.Split(appendix, "\n") {
		if strings.HasPrefix(l, "| Tifa |") && strings.Contains(l, " s |") && (strings.Contains(l, "window") || strings.Contains(l, "target")) {
			tries = append(tries, l)
		}
	}
	if len(tries) != 5 {
		t.Fatalf("the appendix lists %d capped tries, want 5:\n%s", len(tries), strings.Join(tries, "\n"))
	}
	inWindow, short, invalid := 0, 0, 0
	for _, l := range tries {
		switch {
		case strings.Contains(l, "| 10 |") && strings.Contains(l, "90 s") && strings.Contains(l, "in the window"):
			inWindow++
		case strings.Contains(l, "| 13 |") && strings.Contains(l, "40 s") && strings.Contains(l, "shorter than the window"):
			short++
			if strings.Contains(l, "not valid") {
				invalid++
			}
		}
	}
	if inWindow != 1 || short != 4 || invalid != 1 {
		t.Errorf("capped tries: %d in the window, %d short, %d of them not valid, want 1, 4 and 1:\n%s", inWindow, short, invalid, strings.Join(tries, "\n"))
	}
}

// Tifa with its render memo off is a reference in both tables of the body, named as one and kept out
// of the comparison: what Tifa costs when nothing repeats.
func TestTheMemoOffTifaIsAReferenceInBothTables(t *testing.T) {
	sc := mustScenario("shop")
	servers := []string{"tifa", "nextjs", "tifa-nocache"}
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: servers}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"},
		Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}, LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}}
	for _, f := range servers {
		r.Throughput[sustainKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
		x := closedRound(f, sc.Name, 8, 1, 4000, 10, true)
		r.Closed = append(r.Closed, x)
		l := x
		l.Model, l.Rate, l.Sessions = "open", 8, 0
		r.Latency = append(r.Latency, l)
	}
	body, _, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	tput, lat, _ := strings.Cut(body, "#### Latency at the same arrival rate")
	for name, table := range map[string]string{"throughput": tput, "latency": lat} {
		if !strings.Contains(table, "| Tifa, memo off (reference) |") {
			t.Errorf("the %s table has no reference row of Tifa with its memo off:\n%s", name, table)
		}
		if !strings.Contains(table, "Tifa, memo off (reference) is Tifa with `--ssr-memo-size 0`") || !strings.Contains(table, "not one of the frameworks compared") {
			t.Errorf("the %s table does not say what the reference is:\n%s", name, table)
		}
	}
	// The reference comes after the frameworks compared.
	if i, j := strings.Index(tput, "| Tifa, memo off (reference) |"), strings.Index(tput, "| Next.js |"); i < j {
		t.Errorf("the reference is listed before Next.js:\n%s", tput)
	}
}

// The browse comparison runs Tifa as it ships, the three Node frameworks, and Tifa with its render memo
// off as a reference, which replays the trace recorded against Tifa: recording records each trace
// once, from the spec of the framework, and browse.sh names the same trace files.
func TestTheBrowseServersAndTheirTraces(t *testing.T) {
	if got := strings.Join(BrowseFrameworks, ","); got != "tifa,nextjs,start,reactrouter,tifa-nocache" {
		t.Errorf("the browse servers are %s", got)
	}
	env := Env{Shop: "/shop"}
	for _, s := range env.Specs() {
		if fw, _, _ := strings.Cut(s.Name, "-"); fw != s.Framework || traceName(s) != fw {
			t.Errorf("spec %s of framework %s has the trace %s: browse.sh names the trace of a spec by its id before the first dash", s.Name, s.Framework, traceName(s))
		}
	}
	got, err := env.recorded([]string{"tifa-nocache", "nextjs", "tifa", "start"})
	if err != nil || strings.Join(got, ",") != "tifa,nextjs,start" {
		t.Errorf("records %v (%v), want tifa,nextjs,start", got, err)
	}
	if _, err := env.recorded([]string{"nope"}); err == nil {
		t.Error("an unknown spec is recorded")
	}
	data, err := os.ReadFile(filepath.Join("browse.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "servers=${SERVERS:-"+strings.Join(BrowseFrameworks, ",")+"}") {
		t.Error("browse.sh does not run the servers of BrowseFrameworks by default")
	}
}

// A server that compressed no answer is named above both tables of the body, with its body bytes a
// page view against those of the servers that compressed, as the rounds measured them: the numbers
// come from the result.
func TestTheTablesSayWhichServerSendsNoCompression(t *testing.T) {
	sc := mustScenario("shop")
	servers := []string{"tifa", "nextjs", "start", "reactrouter"}
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: servers}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"},
		Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}, LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}}
	kb := map[string]int64{"tifa": 14, "nextjs": 16, "start": 208, "reactrouter": 87}
	for _, f := range servers {
		r.Throughput[sustainKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
		x := closedRound(f, sc.Name, 8, 1, 4000, 10, true)
		x.Bytes = kb[f] * 1024 * int64(x.PageViews)
		x.Encodings = map[string]int{"gzip": 900, "identity": 100}
		if f == "start" {
			x.Encodings = map[string]int{"identity": 1000}
		}
		r.Closed = append(r.Closed, x)
		l := x
		l.Model, l.Rate, l.Sessions = "open", 8, 0
		l.Bytes = l.Bytes / 2
		r.Latency = append(r.Latency, l)
	}
	body, _, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	tput, lat, _ := strings.Cut(body, "#### Latency at the same arrival rate")
	for name, tc := range map[string]struct{ table, want string }{
		"throughput": {tput, "**TanStack Start compresses no answer: 208.0 KB of body a page view, against 14.0 to 87.0 KB for the others, which send gzip.**"},
		"latency":    {lat, "**TanStack Start compresses no answer: 104.0 KB of body a page view, against 7.0 to 43.5 KB for the others, which send gzip.**"},
	} {
		if i, j := strings.Index(tc.table, tc.want), strings.Index(tc.table, "| --- |"); i < 0 || i > j {
			t.Errorf("the %s table does not say above its rows %q:\n%s", name, tc.want, tc.table)
		}
	}
	// A result without the encodings (an older one) says nothing about them.
	for i := range r.Closed {
		r.Closed[i].Encodings = nil
	}
	for i := range r.Latency {
		r.Latency[i].Encodings = nil
	}
	if md := r.Markdown(); strings.Contains(md, "compresses no answer") {
		t.Errorf("a result without encodings names a server that compresses nothing")
	}
}
