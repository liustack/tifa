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

func closedRound(fw, sc string, users, round int, views int, seconds float64, pass bool) Round {
	return Round{Framework: fw, Scenario: sc, Model: "closed", Users: users, Round: round, Blocks: 4, Visits: views / 4, PageViews: views, FullLoads: views / 4, Clicks: views * 3 / 4,
		Writes: views / 19, WriteShare: 0.05, ClickShare: 0.75, ViewsPerVisit: 4, Seconds: seconds, DrainSeconds: 1.5, CPUSeconds: seconds / 2, PeakRSS: 100, Requests: views * 2, Bytes: int64(views) * 4096,
		Responses: map[string]int{"doc 200 text/html": views / 4, "nav 200 application/json": views * 3 / 4, "write 200 application/json": views / 19},
		Renders:   map[string]int{"vm": 3, "cache": views/4 - 3},
		Types:     map[string]TypeStat{"load": {N: 100, P50: 4, P99: 30}, "click": {N: 300, P50: 2, P99: 15}, "write": {N: 20, P50: 5, P99: 9}},
		Valid:     true, Pass: pass, Why: map[bool]string{true: "", false: "click P99 1200 ms over 1000 ms"}[pass]}
}

func TestBrowseReportShowsFailedCellsAndTheLatencyRule(t *testing.T) {
	mix := mustScenario("store-w5")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{
		"store-w5/nextjs": {Rate: 16, Done: true}},
		LatencyRate: map[string]float64{"store-w5": 8}, LatencyBase: map[string]string{"store-w5": "nextjs"}}
	for round := 1; round <= 3; round++ {
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 32, round, 4000, 10, true), closedRound("nextjs", mix.Name, 32, round, 400, 10, round != 2))
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 8, round, 3000, 10, true), closedRound("nextjs", mix.Name, 8, round, 380, 10, true))
		l := closedRound("tifa", mix.Name, 0, round, 1000, 100, true)
		l.Framework, l.Model, l.Rate = "tifa", "open", 8
		r.Latency = append(r.Latency, l)
	}
	md := r.Markdown()
	for _, want := range []string{
		"### storefront, reads 95% / writes 5%",
		"| | 8 users | 32 users |",
		"| Tifa | 300.0 | 400.0 |",
		"| Next.js | 38.0 | failed (click P99 1200 ms over 1000 ms) |", // one of three rounds failed: the cell failed
		"#### The arrival rate Next.js sustains",
		"| Next.js | 16.00 | 64 |",
		"half of the rate Next.js sustains on this scenario",
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
	mix := mustScenario("store-w5")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"},
		LatencyRate: map[string]float64{"store-w5": 8}, LatencyBase: map[string]string{"store-w5": "nextjs"},
		Sustain: map[string]*Sustain{"store-w5/nextjs": {Rate: 16, Done: true}}}
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
		"| Tifa | 4.0 / 30 (300) | 2.0 / 15 (900) | 5.0 / 9 (60) | 0 |", "failed the rule in 3 of 3 rounds: round 1 failed: click P99 1200 ms over 1000 ms"} {
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
	run := func(scenarios string, args ...string) string {
		cmd := exec.Command("bash", append([]string{filepath.Join(env.Root, "test", "compare", "browse.sh")}, args...)...)
		cmd.Dir = env.Root
		cmd.Env = append(os.Environ(), "BROWSE_OUT="+out, "SCENARIOS="+scenarios)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("browse.sh %s: %v\n%s", args, err, data)
		}
		return string(data)
	}
	trace := run("admin-w20", "stamp")
	for _, want := range []string{"tree js ", "tree internal ", "tree server ", "tree cmd ", "tree root-go ", "tree test/compare/recorder ", "tree test/compare/plan.go ", "tree test/compare/store_plan.go ", "tree test/compare/store_links.json ", "tree examples/shop ", "tree test/compare/nextjs ", "tree test/compare/start ", "tree test/compare/reactrouter ", "playwright ", "site workbench\n", "plans none\n"} {
		if !strings.Contains(trace, want) {
			t.Errorf("the trace stamp lacks %q:\n%s", want, trace)
		}
	}
	// The storefront has a stamp of its own, bound to its own plans.
	os.MkdirAll(filepath.Join(out, "traces"), 0o755)
	os.WriteFile(filepath.Join(out, "traces", "plans-store.json"), []byte("{}"), 0o644)
	store := run("admin-w20", "stamp", "store")
	if !strings.Contains(store, "site store\n") || strings.Contains(store, "plans none") {
		t.Errorf("the storefront stamp:\n%s", store)
	}
	if !strings.Contains(run("admin-w20", "stamp"), "plans none\n") {
		t.Error("the workbench stamp is bound to the plans of the storefront")
	}
	results := run("admin-w20", "runstamp")
	for _, want := range []string{"traces ", "trace-files ", "replayer ", "replayer-cmd ", "shop-binary ", "next-output ", "start-output ", "reactrouter-output ", "tifa-build ", "settings ", "scenarios=admin-w20 ", "go ", "node "} {
		if !strings.Contains(results, want) {
			t.Errorf("the result stamp lacks %q:\n%s", want, results)
		}
	}
	// A result with the storefront scenarios is bound to the storefront traces too.
	first := func(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }
	if first(run("admin-w20,store-w5", "runstamp")) == first(results) {
		t.Error("the result stamp of a run with the storefront is not bound to its traces")
	}
	// The script names the scenarios of the groups as replay.go does.
	for group := range ScenarioGroups {
		scs, _ := ParseScenarios(group + ",r95w5")
		if got, want := strings.TrimSpace(run(group+",r95w5", "scenarios")), strings.Join(scenarioNamesOf(scs), ","); got != want {
			t.Errorf("browse.sh expands %s to %s, replay.go to %s", group, got, want)
		}
	}
}

// Every scenario gets the tables it has, with its own servers: the closed tables for a scenario with
// throughput, the search and the latency for one with latency, each with its own rate. The writes of
// a round are given for the rounds a scenario has.
func TestBrowseReportHasTheTablesOfEveryScenario(t *testing.T) {
	scs, _ := ParseScenarios("default,extra")
	r := &BrowseResult{Scenarios: scs, Servers: map[string][]string{}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{},
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
			x.WriteKinds = map[string]int{WriteCreate: 40, WriteShip: 30, WriteAdd: 50, WriteCheckout: 20}
			x.Orders = map[string]int{"studio": 82, "weekend": 12}
			x.PoolEmpty = 21 // of 210 writes
			if sc.Throughput {
				r.Closed = append(r.Closed, x)
			}
			if sc.Latency {
				l := x
				l.Model, l.Rate, l.Users = "open", float64(i+2), 0
				r.Latency = append(r.Latency, l)
			}
		}
	}
	md := r.Markdown()
	sections := strings.Split(md, "\n### ")
	for i, sc := range scs {
		var sec string
		for _, s := range sections {
			if strings.HasPrefix(s, sc.Title+"\n") {
				sec = s
			}
		}
		if sec == "" {
			t.Fatalf("no section for %s:\n%s", sc.Title, md)
		}
		closed := []string{"#### Page views a second at the same number of users", "#### 32 users"}
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
		switch sc.Mix {
		case MixAdmin:
			if !strings.Contains(sec, "| place an order | ship |") || !strings.Contains(sec, "| 82 / 12 |") || !strings.Contains(sec, "| 21 (10.0%) |") {
				t.Errorf("%s lacks the table of admin writes:\n%s", sc.Name, sec)
			}
		case MixStore:
			if !strings.Contains(sec, "| add to cart |") {
				t.Errorf("%s lacks the table of store writes:\n%s", sc.Name, sec)
			}
		}
	}
	for _, want := range []string{"In the admin scenarios", "The storefront scenarios replay"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q", want)
		}
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

// For Tifa the report splits the render cache hits of full loads by kind of page, in every closed
// run and in the read-only control of admin-w5: a diagnostic of what the writes do to the cache.
func TestBrowseReportSplitsTheRenderCacheOfFullLoadsByPage(t *testing.T) {
	sc := mustScenario("store-w5")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{}}
	x := closedRound("tifa", sc.Name, 32, 1, 4000, 10, true)
	x.PageRenders = map[string]map[string]int{"list": {"cache": 55, "vm": 45}, "stats": {"cache": 11, "vm": 9}, "order": {"cache": 1, "vm": 99}}
	n := closedRound("nextjs", sc.Name, 32, 1, 400, 10, true)
	c := closedRound("tifa", sc.Name, 32, 1, 8000, 10, true)
	c.Model, c.Writes, c.WriteShare = "control", 0, 0
	c.PageRenders = map[string]map[string]int{"list": {"cache": 98, "vm": 2}, "stats": {"cache": 19, "vm": 1}, "order": {"cache": 97, "vm": 3}}
	r.Closed, r.Control = []Round{x, n}, []Round{c}
	md := r.Markdown()
	for _, want := range []string{
		"render cache hits of full loads by kind of page",
		"| | list | order | stats |",
		"| Tifa | 55.0% of 100 | 1.0% of 100 | 55.0% of 20 |",
		"#### Read-only control",
		"| Tifa, no writes | 800.0 | 98.0% of 100 | 97.0% of 100 | 95.0% of 20 |",
		"| Tifa, 5% writes | 400.0 | 55.0% of 100 | 1.0% of 100 | 55.0% of 20 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| Next.js | 55.0%") {
		t.Error("the split is given for a server that is not Tifa")
	}
}

// The closed rows give the server cores next to the client cores, and the saturation table the users
// at which each server saturated, from this run or from the file.
func TestBrowseReportShowsServerCoresAndWhereEachServerSaturated(t *testing.T) {
	sc := mustScenario("store-w20")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "start"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{},
		Saturated: map[string]*Saturated{
			"store-w20/tifa":   {Users: 50, Why: "used 0.96 of 1 cpus at 50 users", Steps: []int{40, 50}, Probed: true},
			"store-w20/nextjs": {Users: 32, Why: "at 40 users the page views a second rose by less than the spread of the rounds", Tifa: "abc1234", Date: "2026-11-01"},
		}}
	for round, d := range []int{-10, 0, 10} {
		r.Closed = append(r.Closed, closedRound("tifa", sc.Name, 32, round+1, 4000+d, 10, true), closedRound("nextjs", sc.Name, 32, round+1, 400+d, 10, true), closedRound("start", sc.Name, 32, round+1, 300+d, 10, true))
		r.Saturation = append(r.Saturation, closedRound("tifa", sc.Name, 40, round+1, 4500+d, 10, true), closedRound("tifa", sc.Name, 50, round+1, 5000+d, 10, true))
	}
	md := r.Markdown()
	for _, want := range []string{
		"| | 32 users |\n",
		"| Tifa | 400.0 (399.0 to 401.0) |\n",
		"| server cores | client cores |",
		"| 0.50 | - |",
		"#### Users at which each server saturated",
		"(40, 50, 64 and so on)",
		"| Tifa | 50 | 500.0 (499.0 to 501.0) | 0.50 | 4.0 / 30 (300) | 2.0 / 15 (900) | used 0.96 of 1 cpus at 50 users | in this run |",
		"| Next.js | 32 | 40.0 (39.0 to 41.0) | 0.50 | 4.0 / 30 (300) | 2.0 / 15 (900) | at 40 users the page views a second rose by less than the spread of the rounds | Tifa abc1234, 2026-11-01 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| TanStack Start | 32 |") || strings.Contains(md, "#### 40 users") {
		t.Error("the report shows a server with no saturation entry, or the steps as a closed table")
	}
	r.Saturated = nil
	if md := r.Markdown(); !strings.Contains(md, "Not measured in this run: it needs the server pinned to cpus") {
		t.Errorf("the report does not say the saturation was not measured:\n%s", md)
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

func scenarioNamesOf(list []BrowseScenario) []string {
	var out []string
	for _, sc := range list {
		out = append(out, sc.Name)
	}
	return out
}
