package compare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func closedRound(fw, mix string, users, round int, views int, seconds float64, pass bool) Round {
	return Round{Framework: fw, Mix: mix, Model: "closed", Users: users, Round: round, Blocks: 4, Visits: views / 4, PageViews: views, FullLoads: views / 4, Clicks: views * 3 / 4,
		Writes: views / 19, WriteShare: 0.05, ClickShare: 0.75, ViewsPerVisit: 4, Seconds: seconds, DrainSeconds: 1.5, CPUSeconds: seconds / 2, PeakRSS: 100, Requests: views * 2, Bytes: int64(views) * 4096,
		Responses: map[string]int{"doc 200 text/html": views / 4, "nav 200 application/json": views * 3 / 4, "write 200 application/json": views / 19},
		Renders:   map[string]int{"vm": 3, "cache": views/4 - 3},
		Types:     map[string]TypeStat{"load": {N: 100, P50: 4, P99: 30}, "click": {N: 300, P50: 2, P99: 15}, "write": {N: 20, P50: 5, P99: 9}},
		Valid:     true, Pass: pass, Why: map[bool]string{true: "", false: "click P99 1200 ms over 1000 ms"}[pass]}
}

func TestBrowseReportShowsFailedCellsAndTheLatencyRule(t *testing.T) {
	mix := Mixes[0]
	r := &BrowseResult{Mixes: []Mix{mix}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"}, Sustain: map[string]*Sustain{
		"tifa": {Rate: 400, Done: true}, "nextjs": {Rate: 16, Done: true}},
		LatencyRate: 8, LatencyBase: "nextjs"}
	for round := 1; round <= 3; round++ {
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 32, round, 4000, 10, true), closedRound("nextjs", mix.Name, 32, round, 400, 10, round != 2))
		r.Closed = append(r.Closed, closedRound("tifa", mix.Name, 8, round, 3000, 10, true), closedRound("nextjs", mix.Name, 8, round, 380, 10, true))
		l := closedRound("tifa", mix.Name, 0, round, 1000, 100, true)
		l.Framework, l.Model, l.Rate = "tifa", "open", 8
		r.Latency = append(r.Latency, l)
	}
	md := r.Markdown()
	for _, want := range []string{
		"| | reads 95% / writes 5%, 8 users | reads 95% / writes 5%, 32 users |",
		"| Tifa | 300.0 | 400.0 |",
		"| Next.js | 38.0 | failed (click P99 1200 ms over 1000 ms) |", // one of three rounds failed: the cell failed
		"The slowest framework (Next.js) therefore sets the rate",
		"Every framework gets 8.00 visits/s",
		"| Tifa | 400.00 | 1600 |",
		"What one visit costs the server",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
}

func TestLatencyTableLeavesFailedRoundsOutAndSaysSo(t *testing.T) {
	mix := Mixes[0]
	r := &BrowseResult{Mixes: []Mix{mix}, Date: "2026-11-06", Version: map[string]string{"go": "go1.27"}, LatencyRate: 8, LatencyBase: "nextjs",
		Sustain: map[string]*Sustain{"nextjs": {Rate: 16, Done: true}, "tifa": {Done: true, Note: "no rate passed"}}}
	for round := 1; round <= 3; round++ {
		a := closedRound("nextjs", mix.Name, 0, round, 1000, 100, round != 2)
		a.Model = "open"
		if round == 2 {
			a.TimedOut = true
			a.Why = "the workload did not finish in time: a backlog was left"
			a.Types["load"] = TypeStat{N: 5, P50: 3000, P99: 3000}
		}
		r.Latency = append(r.Latency, a)
		b := closedRound("tifa", mix.Name, 0, round, 1000, 100, false)
		b.Model, b.Framework = "open", "tifa"
		r.Latency = append(r.Latency, b)
	}
	md := r.Markdown()
	for _, want := range []string{"| Next.js | 4.0 / 30 (200)", "2 of 3 rounds, the failed ones left out: round 2 failed: the workload did not finish in time", "| Tifa | failed | failed | failed |", "| Tifa | none in range | - | no rate passed |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "3000") {
		t.Error("a failed round's percentiles are in the table")
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
	run := func(arg string) string {
		cmd := exec.Command("bash", filepath.Join(env.Root, "test", "compare", "browse.sh"), arg)
		cmd.Dir = env.Root
		cmd.Env = append(os.Environ(), "BROWSE_OUT="+t.TempDir())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("browse.sh %s: %v\n%s", arg, err, out)
		}
		return string(out)
	}
	trace := run("stamp")
	for _, want := range []string{"tree js ", "tree internal ", "tree server ", "tree cmd ", "tree root-go ", "tree test/compare/recorder ", "tree test/compare/plan.go ", "tree examples/shop ", "tree test/compare/nextjs ", "tree test/compare/start ", "tree test/compare/reactrouter ", "playwright ", "plans "} {
		if !strings.Contains(trace, want) {
			t.Errorf("the trace stamp lacks %q:\n%s", want, trace)
		}
	}
	results := run("runstamp")
	for _, want := range []string{"traces ", "trace-files ", "replayer ", "replayer-cmd ", "shop-binary ", "next-output ", "start-output ", "reactrouter-output ", "tifa-build ", "settings ", "go ", "node "} {
		if !strings.Contains(results, want) {
			t.Errorf("the result stamp lacks %q:\n%s", want, results)
		}
	}
}
