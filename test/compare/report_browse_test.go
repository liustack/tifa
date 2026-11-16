package compare

import (
	"fmt"
	"maps"
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
		Types:     map[string]TypeStat{"load": {N: 100, P50: 4, P99: 30}, "click": {N: 300, P50: 2, P99: 15}, "document": {N: views * 3 / 10, P50: 3, P99: 35}, "click nav": {N: views - views*3/10, P50: 1, P99: 8}, "write": {N: 20, P50: 5, P99: 9}},
		Valid:     true, Pass: pass, Why: map[bool]string{true: "", false: "click P99 1200 ms over 1000 ms"}[pass]}
}

func TestBrowseReportShowsFailedCellsAndTheLatencyRule(t *testing.T) {
	mix := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"},
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
		"#### Latency at the same arrival rate",
		"40% of the median 10.00 visits a second Next.js completed in its 3 closed rounds at 32 sessions, to the nearest 0.25",
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

// The latency table gives the round a server played at the rate, a failed one too: a server whose
// round failed fails the rule, and its errors and tail latency are in its row.
func TestLatencyTableKeepsFailedRoundsAndSaysSo(t *testing.T) {
	mix := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"},
	}
	a := closedRound("nextjs", mix.Name, 0, 1, 1000, 100, false)
	a.Model, a.TimedOut, a.Failures = "open", true, 20
	a.Why = "the workload did not finish in time: a backlog was left"
	a.Types["document"] = TypeStat{N: 5, P50: 3000, P99: 3000}
	b := closedRound("tifa", mix.Name, 0, 1, 1000, 100, true)
	b.Model = "open"
	r.Latency = append(r.Latency, a, b)
	md := r.Markdown()
	for _, want := range []string{
		"| Next.js | 3000.0 / 3000 (5) |", "| 20 |",
		"failed the rule: the workload did not finish in time",
		"| Tifa | 3.0 / 35 (300) | 1.0 / 8 (700) | passed |", "| Tifa | 0 |"} {
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
	// Every trace has a stamp of its own: what all the traces share (the shop, whose pages every app
	// serves, the recorder and its browser, the code that makes the plans, the site and its plans),
	// and what that framework alone is made of. So a change to Tifa leaves the traces of the Node apps
	// current, a change to one Node app leaves the others current, and a change to the shop none.
	shared := []string{"tree examples/shop ", "tree test/compare/recorder ", "tree test/compare/plan.go ", "tree test/compare/landing.go ", "tree test/compare/mix.go ", "tree test/compare/store_plan.go ", "tree test/compare/store_actions.go ", "tree test/compare/store_list.go ", "tree test/compare/store_terms.go ", "tree test/compare/store.go ", "tree test/compare/store_links.json ", "playwright ", "site shop\n", "plans none\n"}
	tifa := []string{"tree js ", "tree internal ", "tree server ", "tree cmd ", "tree go.mod ", "tree go.sum ", "tree root-go "}
	next := []string{"tree test/compare/nextjs ", "tree test/compare/domain ", "tree test/compare/overlay ", "next "}
	start := []string{"tree test/compare/start ", "tree test/compare/domain ", "tree test/compare/overlay ", "start "}
	for name, own := range map[string][]string{"tifa": tifa, "nextjs": next, NextNoPrefetch: next, "start": start} {
		stamp := run("stamp", name)
		for _, want := range append(slices.Clone(shared), own...) {
			if !strings.Contains(stamp, want) {
				t.Errorf("the stamp of the trace of %s lacks %q:\n%s", name, want, stamp)
			}
		}
		for _, other := range slices.Concat(tifa, next, start) {
			if !slices.Contains(own, other) && strings.Contains(stamp, "\n"+other) {
				t.Errorf("the stamp of the trace of %s has %q, which it does not depend on:\n%s", name, other, stamp)
			}
		}
	}
	// browse.sh stamp alone prints the stamp of every trace the servers replay, each under its name.
	all := run("stamp")
	for _, name := range []string{"tifa", "nextjs", "start", NextNoPrefetch} {
		if !strings.Contains(all, "# "+name+"\n") {
			t.Errorf("browse.sh stamp lacks the trace of %s:\n%s", name, all)
		}
	}
	// The stamp is bound to the plans of the site.
	os.MkdirAll(filepath.Join(out, "traces"), 0o755)
	os.WriteFile(filepath.Join(out, "traces", "plans.json"), []byte("{}"), 0o644)
	if strings.Contains(run("stamp", "nextjs"), "plans none") {
		t.Error("the stamp is not bound to the plans")
	}
	// A result has a stamp all its servers share (the plans, the replayer, the toolchain and the
	// settings) and one for each server: the stamp of its trace, its trace file and the build it runs.
	results := run("runstamp")
	sections := map[string]string{}
	for _, part := range strings.Split(results, "# ")[1:] {
		name, body, _ := strings.Cut(part, "\n")
		sections[name] = body
	}
	for name, wants := range map[string][]string{
		"shared":       {"plans ", "replayer ", "replayer-cmd ", "settings ", "go ", "node "},
		"tifa":         {"trace ", "trace-file ", "shop-binary ", "tifa-build "},
		"tifa-nocache": {"trace ", "trace-file ", "shop-binary ", "tifa-build "},
		"nextjs":       {"trace ", "trace-file ", "next-output "},
		NextNoPrefetch: {"trace ", "trace-file ", "next-noprefetch-output "},
		"start":        {"trace ", "trace-file ", "start-output "},
		"start-gzip":   {"trace ", "trace-file ", "start-output "},
	} {
		body, ok := sections[name]
		if !ok {
			t.Errorf("the result stamp has no part %s:\n%s", name, results)
		}
		for _, want := range wants {
			if !strings.HasPrefix(body, want) && !strings.Contains(body, "\n"+want) {
				t.Errorf("the %s part of the result stamp lacks %q:\n%s", name, want, body)
			}
		}
	}
	for _, line := range []string{"shop-binary ", "next-output ", "start-output ", "trace-file "} {
		if strings.Contains(sections["shared"], line) {
			t.Errorf("the shared part of the result stamp has %q, which belongs to one server:\n%s", line, sections["shared"])
		}
	}
	if strings.Contains(sections["nextjs"], "shop-binary ") || strings.Contains(sections["tifa"], "next-output ") {
		t.Errorf("a server is bound to the build of another:\n%s", results)
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
	r := &BrowseResult{Scenarios: scs, Servers: map[string][]string{}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "32"},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{}}
	for i, sc := range scs {
		servers := BrowseFrameworks
		r.Servers[sc.Name] = servers
		if sc.Latency {
			r.LatencyRate[sc.Name], r.LatencyBase[sc.Name] = float64(i+2), "nextjs"
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
		open := []string{"#### Latency at the same arrival rate", fmt.Sprintf("Every server gets %.2f visits/s", float64(i+2))}
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
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "32"}}
	x := closedRound("tifa", sc.Name, 32, 1, 4000, 10, true)
	x.PageRenders = map[string]map[string]int{"list": {"memo": 55, "vm": 45}, "stats": {"memo": 11, "vm": 9}, "order": {"memo": 1, "vm": 99}, "cart": {"memo": 3, "vm": 7}}
	x.PrivateHits = 3
	x.Memo = &MemoRound{Start: MemoCounts{Hits: 4}, Warm: MemoCounts{Hits: 10}, End: MemoCounts{Hits: 260}} // 250 hits in 1000 visits after the warm-up
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
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "start"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"},
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
		"| | result | sessions | page views/s | full loads/s | client navigations/s | CPU ms/page view | body KB/page view | peak RSS MiB |",
		"| | server cores | why the steps stopped | sessions played |",
		"| Tifa | core used | 13 | 500.0 (499.0 to 501.0) | 150.0 (149.7 to 150.3) | 350.0 (349.3 to 350.7) | 1.00 | 4.0 | 100 |", "| Tifa | 0.50 |",
		"| Next.js | highest passing step | 8 | 40.0 (39.0 to 41.0) | 12.0 (11.7 to 12.3) | 28.0 (27.3 to 28.7) | 12.50 (12.20 to 12.82) | 4.0 | 100 |", "| Next.js | 0.50 |",
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
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"}}
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
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}}
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
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: {"tifa", "nextjs"}}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"},
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
	r := &BrowseResult{Scenarios: scs, Servers: map[string][]string{}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "8"},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{}, Throughput: map[string]*Throughput{}}
	for _, sc := range scs {
		r.Servers[sc.Name] = []string{"tifa", "nextjs"}
		r.LatencyRate[sc.Name], r.LatencyBase[sc.Name] = 4, "nextjs"
		for _, f := range r.Servers[sc.Name] {
			r.Throughput[serverKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
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
		want = append(want, "| | result | sessions | page views/s | full loads/s | client navigations/s | CPU ms/page view | body KB/page view | peak RSS MiB |",
			"| | full loads P50 / P99 ms (n) | client navigations P50 / P99 ms (n) | rule |")
	}
	want = append(want, "| | value | source |")
	if !slices.Equal(heads, want) {
		t.Errorf("the tables of the body have the columns\n%s\nwant\n%s", strings.Join(heads, "\n"), strings.Join(want, "\n"))
	}
	// The two tables have the columns full loads and client navigations, and the text over the first
	// says which pages are which: the landings and the clicks, which add up to the page views. The body
	// never calls them clicks.
	if n := strings.Count(body, stepClassNote); n != len(scs) || !strings.Contains(stepClassNote, "A client navigation is") || !strings.Contains(stepClassNote, "add up to the page views") || !strings.Contains(stepClassNote, "answered with a whole page") || !strings.Contains(stepClassNote, "with data alone") || strings.Contains(stepClassNote, "first page of a visit") {
		t.Errorf("the body says %d times what a full load and a client navigation are, want over the throughput table of each of its %d scenarios: %q", n, len(scs), stepClassNote)
	}
	if tables, _, _ := strings.Cut(body, "\n### The mix\n"); strings.Contains(tables, "click") {
		t.Errorf("the result tables say click:\n%s", tables)
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
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-08", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "32"}}
	x := closedRound("tifa", sc.Name, 32, 1, 4000, 10, true)
	x.Memo = &MemoRound{Start: MemoCounts{Hits: 4}, Warm: MemoCounts{Hits: 10}, End: MemoCounts{Hits: 260}} // 250 hits in 1000 visits after the warm-up
	r.Closed = []Round{x, closedRound("nextjs", sc.Name, 32, 1, 400, 10, true)}
	md := r.Markdown()
	if !strings.Contains(md, "| memo hits Tifa counted |") || !slices.ContainsFunc(strings.Split(md, "\n"), func(l string) bool {
		return strings.HasPrefix(l, "| Tifa |") && strings.HasSuffix(l, " | 0.250 |")
	}) {
		t.Errorf("the report lacks the hits Tifa counted:\n%s", md)
	}
}

// The latency table of the body lists every server of the scenario, whatever its round was: one with
// no valid try has no valid result, one whose valid round passed passed, and one whose valid round
// failed failed.
func TestTheLatencyTableListsEveryServerWithWhatItsRoundsDecide(t *testing.T) {
	mix := mustScenario("shop")
	fws := []string{"tifa", "nextjs", "start", "tifa-nocache"}
	r := &BrowseResult{Scenarios: []BrowseScenario{mix}, Servers: map[string][]string{mix.Name: fws}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Config: map[string]string{"rounds": "3"},
	}
	open := func(fw string, round int, valid, pass bool) Round {
		x := closedRound(fw, mix.Name, 0, round, 1000, 100, pass)
		x.Model, x.Rate, x.Valid = "open", 8, valid
		if !valid {
			x.WhyInvalid = "the load generator started a visit 400 ms late"
		}
		return x
	}
	for try := 1; try <= 3; try++ {
		x := open("tifa", 1, false, true) // every try not valid
		x.Attempt = try
		r.Latency = append(r.Latency, x)
	}
	invalid := open("start", 1, false, true)
	r.Latency = append(r.Latency, open("nextjs", 1, true, true), open("tifa-nocache", 1, true, false), invalid, open("start", 1, true, true))
	md := r.Markdown()
	body, _, _ := strings.Cut(md, "\n## Appendix\n")
	row := func(label string) string {
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, "| "+label+" |") && (strings.Contains(l, "passed") || strings.Contains(l, "failed the rule") || strings.Contains(l, "no valid result")) {
				return l
			}
		}
		return ""
	}
	for label, want := range map[string]string{
		"Tifa":                       "| Tifa | - | - | no valid result: none of its 3 tries was valid |",
		"TanStack Start":             "| passed |",
		"Next.js":                    "| passed |",
		"Tifa, memo off (reference)": "| failed the rule: click P99 1200 ms over 1000 ms |",
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
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"},
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
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}}
	for _, f := range servers {
		r.Throughput[serverKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
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
		if strings.Contains(table, "every request") || strings.Contains(table, "every page") {
			t.Errorf("the %s table says the memo-off Tifa renders every page:\n%s", name, table)
		}
		if !strings.Contains(table, "Tifa, memo off (reference) is Tifa with `--ssr-memo-size 0`, which turns off keeping finished renders") || !strings.Contains(table, "navigation data") ||
			!strings.Contains(table, "share a render in progress") || !strings.Contains(table, "answer 304") || !strings.Contains(table, "not one of the frameworks compared") {
			t.Errorf("the %s table does not say what the reference is:\n%s", name, table)
		}
	}
	// The reference comes after the frameworks compared.
	if i, j := strings.Index(tput, "| Tifa, memo off (reference) |"), strings.Index(tput, "| Next.js |"); i < j {
		t.Errorf("the reference is listed before Next.js:\n%s", tput)
	}
}

// The browse comparison runs Tifa as it ships, the two Node frameworks, and three references: Tifa
// with its render memo off and Start with gzip added, which replay the trace of their framework, and
// Next.js built with prefetching off, a framework of its own with its own trace. Recording records each
// trace once, from the spec of the framework, and browse.sh names the same trace files.
func TestTheBrowseServersAndTheirTraces(t *testing.T) {
	if got := strings.Join(BrowseFrameworks, ","); got != "tifa,nextjs,start,tifa-nocache,start-gzip,nextjsnoprefetch" {
		t.Errorf("the browse servers are %s", got)
	}
	env := Env{Shop: "/shop"}
	for _, s := range env.Specs() {
		if fw, _, _ := strings.Cut(s.Name, "-"); fw != s.Framework || traceName(s) != fw {
			t.Errorf("spec %s of framework %s has the trace %s: browse.sh names the trace of a spec by its id before the first dash", s.Name, s.Framework, traceName(s))
		}
	}
	got, err := env.recorded([]string{"tifa-nocache", "nextjs", "tifa", "start", "start-gzip", NextNoPrefetch})
	if err != nil || strings.Join(got, ",") != "tifa,nextjs,start,nextjsnoprefetch" {
		t.Errorf("records %v (%v), want tifa,nextjs,start,nextjsnoprefetch", got, err)
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
	// The reference sends the least, and is not one of the frameworks the range is of.
	servers := []string{"tifa", "nextjs", "start", "tifa-nocache"}
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: servers}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}}
	kb := map[string]int64{"tifa": 14, "nextjs": 87, "start": 208, "tifa-nocache": 2}
	for _, f := range servers {
		r.Throughput[serverKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
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

// The appendix splits, for each Tifa server, what it rendered in a VM by kind of page and why, next to
// the hits, with what the client cannot tell. The body has none of it.
func TestTheAppendixSplitsTheRendersOfTifaByWhy(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "tifa-nocache"}}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"}}
	for round := 1; round <= 2; round++ {
		x := closedRound("tifa", sc.Name, 8, round, 4000, 10, true)
		x.PageRenders = map[string]map[string]int{"product": {"vm": 30, "memo": 60, "shared": 10}, StoreKindCart: {"vm": 10}}
		x.VMWhy = map[string]map[string]int{"product": {RenderFirst: 20, RenderAgain: 10}, StoreKindCart: {RenderPrivate: 4, RenderPrivateCart: 6}}
		r.Closed = append(r.Closed, x, closedRound("nextjs", sc.Name, 8, round, 400, 10, true))
	}
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	for _, want := range []string{
		"| | kind of page | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | memo and shared, measured | VM: URL not seen before | VM: URL seen, data changed | VM: URL seen, data the same | VM: URL answered before, no data version | private page, no cart cookie | private page, cart cookie |",
		"| Tifa | cart | 20 | 0 | 0 | 20 | - | 0.0% of 20 | - | - | - | - | 8 | 12 |",
		"| Tifa | product | 200 | 120 | 20 | 60 | - | 70.0% of 200 | 40 | 0 | 0 | 20 | - | - |",
		"the plans allow a memo that drops nothing",
		"(counted as shared",
		"canceled",
		"received whole with `X-Tifa-Render`",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
		if strings.Contains(body, want) {
			t.Errorf("the body has %q", want)
		}
	}
	if strings.Contains(appendix, "every page renders") {
		t.Error("the appendix says that with the memo off every page renders")
	}
	if strings.Contains(appendix, "| Next.js | product |") {
		t.Error("the split has a row for Next.js, which says nothing of its renders")
	}
}

// A run with the formal settings (FormalBrowse, the cpus of the server and the load pinned, run by
// browse.sh, every server) says nothing about them. Any other run says at the top of the report that
// it is not a formal run, and how it differs.
func TestTheReportSaysWhenARunIsNotFormal(t *testing.T) {
	sc := mustScenario("shop")
	formal := func() *BrowseResult {
		o := FormalBrowse
		o.ServerCPUs, o.LoadCPUs = "0", "1,5"
		return &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: slices.Clone(BrowseFrameworks)}, Version: map[string]string{}, Stamp: "from browse.sh",
			Config: browseConfig(o, "AMD EPYC 7B13", "t1", "m1")}
	}
	if md := formal().Markdown(); strings.Contains(md, "Not a formal run") {
		t.Errorf("a formal run says it is not one:\n%s", md)
	}
	r := formal()
	r.Config["rounds"], r.Config["latencyBlocks"], r.Config["loadCpus"], r.Config["sizing"] = "1", "1", "", "3s"
	r.Stamp = ""
	r.Servers[sc.Name] = []string{"tifa", "nextjs"}
	md := r.Markdown()
	top, _, _ := strings.Cut(md, "### ")
	for _, want := range []string{"**Not a formal run.**", "rounds 1 (formal 3)", "latencyBlocks 1 (formal 3)", "sizing 3s (formal 15s)", "not pinned", "not run by browse.sh", "servers tifa, nextjs (formal " + strings.Join(BrowseFrameworks, ", ") + ")"} {
		if !strings.Contains(top, want) {
			t.Errorf("the top of the report lacks %q:\n%s", want, top)
		}
	}
}

// browse.sh refuses the names of settings that are gone, so that an old command line does not run
// another method: USERS and EXTRA_USERS (the throughput counts sessions), SCENARIOS (one scenario)
// WARM (the warm-up is the same for every server, and SIZING is the length of a sizing run), PROBE,
// TRIAL_WINDOW and MAX_RATE (no search of a rate) and FINAL_WINDOW (one latency round of its blocks).
func TestBrowseShRefusesTheSettingsThatAreGone(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"USERS", "EXTRA_USERS", "SCENARIOS", "WARM", "PROBE", "TRIAL_WINDOW", "MAX_RATE", "FINAL_WINDOW"} {
		cmd := exec.Command("bash", filepath.Join(env.Root, "test", "compare", "browse.sh"), "stamp")
		cmd.Dir = env.Root
		cmd.Env = append(os.Environ(), "BROWSE_OUT="+t.TempDir(), name+"=3s")
		data, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(data), name+" is gone") {
			t.Errorf("browse.sh with %s: %v\n%s", name, err, data)
		}
	}
}

// The appendix gives, for each round of Tifa, what its render memo held after the warm-up and at the
// end (pages and bytes), the pages it dropped to make room in the round, and the hits it counted
// from the clock on, the end less the read after the warm-up, from its metrics.
func TestTheAppendixGivesWhatTheMemoHeldAfterTheWarmUp(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-09", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "8"}}
	x := closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)
	x.Attempt = 1
	x.Memo = &MemoRound{Start: MemoCounts{}, Warm: MemoCounts{Entries: 812, Bytes: 9 << 20, Hits: 3000, Evictions: 5}, End: MemoCounts{Entries: 900, Bytes: 10 << 20, Hits: 5500, Evictions: 12}}
	// A try whose metrics did not answer after the round: what it held at the end is not known, nor
	// are the hits and pages dropped in the round.
	lost := closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)
	lost.Attempt, lost.Valid, lost.WhyInvalid, lost.MemoError = 2, false, "the render memo could not be read after the round: EOF", "EOF"
	lost.Memo = &MemoRound{Warm: MemoCounts{Entries: 812, Bytes: 9 << 20, Hits: 3000, Evictions: 5}}
	// A try that its warm-up stopped before the clock: neither read after the warm-up was made.
	stopped := closedRound("tifa", sc.Name, 8, 1, 0, 0, true)
	stopped.Attempt, stopped.Valid, stopped.SetupError = 3, false, "the shop refused 3 requests of the warm-up with 409 or 422"
	stopped.WhyInvalid = "the setup before the clock failed: " + stopped.SetupError
	stopped.Memo = &MemoRound{}
	r.Closed = []Round{x, lost, stopped, closedRound("nextjs", sc.Name, 8, 1, 400, 10, true)}
	md := r.Markdown()
	_, appendix, _ := strings.Cut(md, "## Appendix")
	for _, want := range []string{
		"| | round | try | valid | pages after the warm-up | MiB after the warm-up | pages at the end | MiB at the end | pages dropped in the round | hits Tifa counted in the round |",
		"| Tifa | closed, 8 sessions, round 1 | 1 | yes | 812 | 9.0 | 900 | 10.0 | 7 | 2500 |",
		"| Tifa | closed, 8 sessions, round 1 | 2 | no | 812 | 9.0 | not read | not read | not read | not read |",
		"| Tifa | closed, 8 sessions, round 1 | 3 | no | not read | not read | not read | not read | not read | not read |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
	}
}

// The appendix gives, for each round of every server, what its warm-up did and the cores the server
// used in the last second of the pause after it, where the cpu of the server was read.
func TestTheAppendixGivesWhatEachWarmUpDid(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Date: "2026-11-10", Version: map[string]string{"go": "go1.27"}, Config: map[string]string{"sessions": "8"}}
	cores := 0.03
	x := closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)
	x.Attempt = 1
	x.WarmUp = &WarmUp{VisitsPlanned: 2000, Visits: 1999, VisitsFailed: 1, WritesPlanned: 60, Writes: 59, Failures: 1, Seconds: 30.2, IdleCores: &cores}
	y := closedRound("nextjs", sc.Name, 8, 1, 400, 10, true)
	y.Attempt, y.Valid = 2, false
	y.WarmUp = &WarmUp{VisitsPlanned: 2000, Visits: 2000, WritesPlanned: 60, Writes: 60, Seconds: 301}
	r.Closed = []Round{x, y}
	_, appendix, _ := strings.Cut(r.Markdown(), "## Appendix")
	for _, want := range []string{
		"#### The warm-up before each round",
		"| | round | try | valid | visits done | writes done | failures | seconds | server cores in the last second before the clock |",
		"| Tifa | closed, 8 sessions, round 1 | 1 | yes | 1999 of 2000 | 59 of 60 | 1 | 30 | 0.03 |",
		"| Next.js | closed, 8 sessions, round 1 | 2 | no | 2000 of 2000 | 60 of 60 | 0 | 301 | - |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
	}
}

// The text on the site says that every server gets the same warm-up, and that Tifa's memo is read
// empty before it, not before the clock.
func TestTheSiteTextSaysEveryServerGetsTheSameWarmUp(t *testing.T) {
	for _, want := range []string{"Then every server gets the same warm-up, counted in no result", "held a page right before the warm-up"} {
		if !strings.Contains(siteText, want) {
			t.Errorf("the site text lacks %q", want)
		}
	}
	if strings.Contains(siteText, "right before the clock") {
		t.Error("the site text still reads the memo right before the clock")
	}
}

// crawled is a round with the crawler requests of a block (101, in the proportion of the stream) done
// in the seconds of the round.
func crawled(x Round) Round {
	x.Crawler = &CrawlStats{Planned: 101, Done: 101, Bytes: 101 * 60_000, OldBytes: 5 * 15_000, Statuses: map[string]int{"U 200": 96, "O 404": 5},
		Renders: map[string]map[string]int{"U|en-US|list": {"memo": 6, "vm": 2}, "U|en-US|product": {"memo": 30, "shared": 2, "vm": 8}, "U|zh-CN|reviews": {"memo": 20, "vm": 20}, "U|zh-CN|home": {"memo": 4}, "O|zh-CN|product": {"vm": 5}}}
	x.Types = maps.Clone(x.Types)
	x.Types["crawler"] = TypeStat{N: 101, P50: 12, P99: 480}
	return x
}

// The tables of the body hold the people alone: the crawler requests, and the latency of the writes and
// of the crawler documents, are in the appendix, with their samples.
func TestTheCrawlerAndTheWritesAreInTheAppendixAlone(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{},
		Config: map[string]string{"sessions": "8"}, LatencyRate: map[string]float64{"shop": 4}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}}
	for _, f := range r.Servers[sc.Name] {
		r.Throughput[serverKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
		for round := 1; round <= 3; round++ {
			x := crawled(closedRound(f, sc.Name, 8, round, 4000, 10, true))
			r.Closed = append(r.Closed, x)
			if round == 1 {
				l := x
				l.Model, l.Rate, l.Sessions = "open", 4, 0
				r.Latency = append(r.Latency, l)
			}
		}
	}
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	tables, _, _ := strings.Cut(body, "\n### The mix\n")
	for _, word := range []string{"crawler", "write P50", "active people"} {
		if strings.Contains(tables, word) {
			t.Errorf("the body has %q:\n%s", word, body)
		}
	}
	for _, want := range []string{
		"| | write P50 / P99 ms (n) | crawler document P50 / P99 ms (n) |",
		"| Tifa | 5.0 / 9 (20) | 12.0 / 480 (101) |",
		"| Next.js | 5.0 / 9 (20) | 12.0 / 480 (101) |",
		"| Tifa | 303 | 303 | 0 | 58.6 | 14.6 | O 404 x15, U 200 x288 | 10.1 | 12.0 / 480 (303) |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
	}
	// A server with no crawler request in its rounds shows none, not 0.
	x := closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)
	if typeCell([]Round{x}, "crawler") != "-" {
		t.Error("a round with no crawler request has a crawler latency")
	}
}

// The appendix says what the crawler requests did in each server (planned, answered as expected,
// failed, the body per request, the answers by status), how Tifa answered them by set (U or O), by
// language and by kind of URL, and how big U is.
func TestTheAppendixListsWhatTheCrawlerRequestsDid(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "tifa-nocache"}}, Version: map[string]string{}, Config: map[string]string{"sessions": "8"}}
	for round := 1; round <= 2; round++ {
		for _, f := range r.Servers[sc.Name] {
			r.Closed = append(r.Closed, crawled(closedRound(f, sc.Name, 8, round, 4000, 10, true)))
		}
	}
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	u, o := CrawlURLs()
	for _, want := range []string{
		fmt.Sprintf("U holds %d URLs", len(u)),
		fmt.Sprintf("O holds %d old URLs", len(o)),
		"| | crawler requests planned | answered as expected | failed | body KB/request | body KB/request of an old URL (404) | answers | crawler requests/s | crawler document P50 / P99 ms (n) |",
		"| Tifa | 202 | 202 | 0 | 58.6 | 14.6 | O 404 x10, U 200 x192 | 10.1 | 12.0 / 480 (202) |",
		"| Next.js | 202 | 202 | 0 | 58.6 | 14.6 | O 404 x10, U 200 x192 | 10.1 | 12.0 / 480 (202) |",
		"Next.js answers an old URL with an error document and renders no page for it on the server",
		"the other two servers render a whole page for it",
		"| | answers by | answers | from the memo | shared a render in progress | rendered in a VM |",
		"| Tifa | U | 184 | 120 | 4 | 60 | - | 67.4% of 184 |",
		"| Tifa | O | 10 | 0 | 0 | 10 | - | 0.0% of 10 |",
		"| Tifa | U, zh-CN | 88 | 48 | 0 | 40 | - | 54.5% of 88 |",
		"| Tifa | U, reviews | 80 | 40 | 0 | 40 | - | 50.0% of 80 |",
		"| Tifa, memo off (reference) | U |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
		if strings.Contains(body, want) {
			t.Errorf("the body has %q", want)
		}
	}
	if strings.Contains(appendix, "| Next.js | U |") {
		t.Error("the split of the answers has a row for Next.js, which says nothing of its renders")
	}
}

// The body of a scenario has a throughput table and, apart from it, a latency table, each with the
// three main servers first and the references after them, and under them the notes: when each
// server was measured, the versions, how many rounds a row is, how the latency round and its rate were
// made, and the check of the machine when one ran. The appendix gives the three parts of each latency
// round.
func TestTheBodyHasAThroughputAndALatencyTableWithTheirNotes(t *testing.T) {
	sc := mustScenario("shop")
	servers := []string{"tifa", "tifa-nocache", "nextjs", "start-gzip", "start"}
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: servers}, Date: "2026-11-14 09:00:00 UTC",
		Version: map[string]string{"go": "go1.27", "node": "24.11.0", "next": "16.0.1"}, Config: map[string]string{"sessions": "8", "rounds": "3"},
		LatencyRate: map[string]float64{"shop": 40}, LatencyBase: map[string]string{"shop": "nextjs"}, Throughput: map[string]*Throughput{}, ServerStamps: map[string]ServerStamp{}}
	for _, f := range servers {
		r.Throughput[serverKey(sc.Name, f)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8, Steps: []int{8}}
		r.ServerStamps[f] = ServerStamp{Stamp: f, Date: "2026-11-15 10:00:00 UTC"}
		for round := 1; round <= closedRounds(f, 3); round++ {
			r.Closed = append(r.Closed, closedRound(f, sc.Name, 8, round, 4000, 10, true))
		}
		l := closedRound(f, sc.Name, 0, 1, 4800, 300, true)
		l.Model, l.Rate, l.VisitsPlanned, l.Blocks = "open", 40, 1200, 3
		for range 3 {
			l.Parts = append(l.Parts, RoundPart{FullLoads: TypeStat{N: 33, P50: 4, P99: 30}, ClientNavs: TypeStat{N: 99, P50: 2, P99: 15}})
		}
		r.Latency = append(r.Latency, l)
	}
	r.ServerStamps["tifa"] = ServerStamp{Stamp: "tifa", Date: "2026-11-14 10:00:00 UTC"}
	r.Closed[3] = closedRound("tifa-nocache", sc.Name, 8, 1, 2000, 10, true) // a reference that does less, so the order is not by speed
	r.MachineChecks = []MachineCheck{{Server: "start", Scenario: sc.Name, Sessions: 8, Values: []float64{81.9, 76.3, 76.5}, Median: 76.5, KeptMedian: 78.8, Diff: -2.92, Date: "2026-11-16 09:00:00 UTC"}}
	md := r.Markdown()
	body, appendix, _ := strings.Cut(md, "\n## Appendix\n")
	tput, lat, ok := strings.Cut(body, "#### Latency at the same arrival rate")
	if !ok {
		t.Fatalf("the body has no latency table:\n%s", body)
	}
	if strings.Contains(tput, "P50") || strings.Contains(tput, "P99") {
		t.Errorf("the throughput table has a latency column:\n%s", tput)
	}
	for _, want := range []string{
		"| Tifa | core used | 8 | 400.0 | 120.0 | 280.0 | 1.25 | 4.0 | 100 |",
		"| Tifa, memo off (reference) | core used | 8 | 200.0 | 60.0 | 140.0 | 2.50 | 4.0 | 100 |",
		"A main server plays 3 rounds a step and a reference one",
	} {
		if !strings.Contains(tput, want) {
			t.Errorf("the throughput table lacks %q:\n%s", want, tput)
		}
	}
	if want := "| Tifa | 3.0 / 35 (1440) | 1.0 / 8 (3360) | passed |"; !strings.Contains(lat, want) {
		t.Errorf("the latency table lacks %q:\n%s", want, lat)
	}
	labels := []string{"| Tifa |", "| Next.js |", "| TanStack Start |", "| Tifa, memo off (reference) |", "| TanStack Start, gzip added (reference) |"}
	for name, table := range map[string]string{"throughput": tput, "latency": lat} {
		at := -1
		for _, l := range labels {
			i := strings.Index(table, l)
			if i <= at {
				t.Errorf("the %s table does not list %s after the rows before it:\n%s", name, l, table)
			}
			at = i
		}
	}
	for _, want := range []string{
		"- Tifa was measured on 2026-11-14 10:00:00 UTC.",
		"- TanStack Start, gzip added (reference) was measured on 2026-11-15 10:00:00 UTC.",
		"- Versions: Go 1.27, Node 24.11.0, next 16.0.1.",
		"- A row of a main server in the throughput table is the median of its 3 rounds at its sessions, with their range. A row of a reference is one round.",
		"- A row of the latency table is one round of 1200 visits (3 blocks) at 40.00 visits/s. The rate is 40% of the median 100.00 visits a second Next.js completed in its 3 closed rounds at 8 sessions, to the nearest 0.25.",
		"- The machine was checked on 2026-11-16 09:00:00 UTC before servers were measured again beside the kept results: 3 closed rounds of TanStack Start at 8 sessions did 81.9, 76.3 and 76.5 page views/s, a median of 76.5 against 78.8 for its kept rounds, -2.9% (within 3%).",
	} {
		if !strings.Contains(lat, want) {
			t.Errorf("the notes under the tables lack %q:\n%s", want, lat)
		}
	}
	if strings.Contains(r.Markdown(), "Go 1.27, Node 24.11.0, next 16.0.1.\n\n### the shop") {
		t.Error("the versions are above the tables")
	}
	for _, want := range []string{
		"| | part | full loads P50 / P99 ms (n) | client navigations P50 / P99 ms (n) |",
		"| Tifa | 1 of 3 | 4.0 / 30 (33) | 2.0 / 15 (99) |",
		"| Tifa | 3 of 3 | 4.0 / 30 (33) | 2.0 / 15 (99) |",
		"| TanStack Start, gzip added (reference) | 2 of 3 | 4.0 / 30 (33) | 2.0 / 15 (99) |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
	}
	r.MachineChecks = nil
	if strings.Contains(r.Markdown(), "machine was checked") {
		t.Error("the notes name a check of the machine that did not run")
	}
}
