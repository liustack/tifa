package compare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// A result is continued only under the settings it was started with: a probe asked for again, other
// server cpus, another seed or cap of the rate, another cpu model, other plans or another version all
// the servers share (the toolchain) must not reuse the rounds of another run, nor a probe the rate of a run without one.
func TestAResultIsContinuedOnlyUnderItsSettings(t *testing.T) {
	o := BrowseOptions{Sessions: 8, Rounds: 3, Sizing: 15 * time.Second, Target: 75 * time.Second, ServerCPUs: "0", ServerCPUCount: 1, Seed: 1}
	version := map[string]string{"node": "v24.1.0", "nextjs": "16.0.1"}
	fws := []string{"tifa", "nextjs"}
	res := &BrowseResult{Config: browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), Version: version}
	if err := canContinue(res, o.Stamp, browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), version, fws); err != nil {
		t.Fatalf("the same settings: %v", err)
	}
	probe := o
	probe.Probe = true
	cpus := o
	cpus.ServerCPUs, cpus.ServerCPUCount = "0,2", 2
	target := o
	target.Target = 30 * time.Second
	seed := o
	seed.Seed = 2
	capped := o
	capped.MaxRate = 64
	for name, cfg := range map[string]map[string]string{
		"-probe": browseConfig(probe, "AMD EPYC 7B13", "t1", "m1"), "-server-cpus": browseConfig(cpus, "AMD EPYC 7B13", "t1", "m1"), "-target": browseConfig(target, "AMD EPYC 7B13", "t1", "m1"),
		"-seed": browseConfig(seed, "AMD EPYC 7B13", "t1", "m1"), "-max-rate": browseConfig(capped, "AMD EPYC 7B13", "t1", "m1"),
		"another cpu": browseConfig(o, "Intel Xeon", "t1", "m1"), "other plans": browseConfig(o, "AMD EPYC 7B13", "t2", "m1"), "another method": browseConfig(o, "AMD EPYC 7B13", "t1", "m2"),
	} {
		err := canContinue(res, o.Stamp, cfg, version, fws)
		if err == nil || !strings.Contains(err.Error(), "remove it to start again") {
			t.Errorf("%s: continued a result of other settings: %v", name, err)
		}
	}
	if err := canContinue(res, o.Stamp, browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), map[string]string{"node": "v24.2.0", "nextjs": "16.0.1"}, fws); err == nil || !strings.Contains(err.Error(), "node") {
		t.Errorf("another Node: %v", err)
	}
	if err := canContinue(res, "other", browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), version, fws); err == nil || !strings.Contains(err.Error(), "remove it to start again") {
		t.Errorf("another shared stamp: %v", err)
	}
	// A version of one framework, or the engine of Tifa, is no reason to refuse the result: it measures
	// the servers of that framework again (TestAResultContinuesWithTheServersWhoseStampsAreTheSame).
	for _, other := range []map[string]string{{"node": "v24.1.0", "nextjs": "16.0.2"}, {"node": "v24.1.0", "nextjs": "16.0.1", engineVersion: "v0.32.0"}} {
		if err := canContinue(res, o.Stamp, browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), other, fws); err != nil {
			t.Errorf("versions %v: %v", other, err)
		}
	}
}

// A continued result keeps what it holds of the servers whose stamps are those of this run and
// measures the others again: a server whose own stamp (its trace or its build), trace file or version
// of its own (the engine for Tifa, the package of its framework) changed, or that the result does not
// know, loses its sizing, its closed rounds and steps, its latency rounds and, for Tifa, the control.
// The others are untouched, and the numbers every server shares (the prepared visits and the warm-up)
// stay. A change to the shared stamp still refuses the result.
func TestAResultContinuesWithTheServersWhoseStampsAreTheSame(t *testing.T) {
	sc := mustScenario("shop")
	tifa, nocache, next := warmSpecs()
	start := next
	start.Name, start.Framework = "start", "start"
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	dir := t.TempDir()
	for _, name := range []string{"plans.json", "tifa.jsonl", "nextjs.jsonl", "start.jsonl"} {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
	}
	stamps := map[string]string{"tifa": "t1", "tifa-nocache": "t1", "nextjs": "n1", "start": "s1"}
	version := map[string]string{"go": "go1.27", "node": "v24.1.0", "tifa": "abc", engineVersion: "v0.31.1", "nextjs": "16.4.2", "start": "1.169.3"}
	cfg := browseConfig(BrowseOptions{}, "", fileHash(PlansPath(dir)), methodHash(root))
	saved := func() []byte {
		b, _ := warmBrowse(t, sc, tifa, nocache, next, start)
		b.e, b.o.TraceDir, b.o.Stamp, b.o.ServerStamps = Env{Root: root}, dir, "shared", stamps
		b.begin(cfg, version, Machine{}, Placement{}, []string{"tifa", "nextjs", "start"})
		for _, fw := range b.servers[sc.Name] {
			for round := 1; round <= 3; round++ {
				b.res.Closed = append(b.res.Closed, closedRound(fw, sc.Name, 8, round, 4000, 100, true))
				b.res.Latency = append(b.res.Latency, Round{Framework: fw, Scenario: sc.Name, Model: "open", Round: round, Valid: true, Pass: true, VisitsPlanned: 4})
			}
			b.res.Blocks[fmt.Sprintf("%s/8/%s", sc.Name, fw)] = 3
			b.res.Throughput = map[string]*Throughput{}
		}
		for _, fw := range b.servers[sc.Name] {
			b.res.Throughput[sustainKey(sc.Name, fw)] = &Throughput{Result: ThroughputCoreUsed, Sessions: 8}
		}
		b.res.Control = []Round{{Framework: "tifa", Scenario: sc.Name, Model: "control", Round: 1, Valid: true, VisitsPlanned: 4}}
		b.res.Prepared, b.res.WarmVisits = map[string]int{sc.Name: 6 * sc.blockVisits()}, map[string]int{sc.Name: 3 * sc.blockVisits()}
		b.res.LatencyRate, b.res.LatencyBase = map[string]float64{sc.Name: 10}, map[string]string{sc.Name: "nextjs"}
		data, err := b.res.json()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	// The result as it was written, before any file of the run changes.
	written := saved()
	restore := func(stamp string, stamps, version map[string]string) (*browse, string, error) {
		b, _ := warmBrowse(t, sc, tifa, nocache, next, start)
		var log strings.Builder
		b.e, b.o.TraceDir, b.o.Stamp, b.o.ServerStamps, b.o.Log = Env{Root: root}, dir, stamp, stamps, &log
		b.res = &BrowseResult{}
		err := b.restore(written, cfg, version)
		return b, log.String(), err
	}
	// kept says whether the result still holds the rounds, sizing and steps of a server.
	kept := func(r *BrowseResult, fw string) bool {
		closed := len(r.cellRounds(fw, sc.Name, 8))
		latency := len(slices.DeleteFunc(slices.Clone(r.Latency), func(x Round) bool { return x.Framework != fw }))
		_, sized := r.Blocks[fmt.Sprintf("%s/8/%s", sc.Name, fw)]
		if closed == 3 && latency == 3 && sized && r.Throughput[sustainKey(sc.Name, fw)] != nil {
			return true
		}
		if closed != 0 || latency != 0 || sized || r.Throughput[sustainKey(sc.Name, fw)] != nil {
			t.Errorf("%s: part of it was dropped: %d closed rounds, %d latency rounds, sized %v", fw, closed, latency, sized)
		}
		return false
	}
	b, log, err := restore("shared", stamps, version)
	if err != nil {
		t.Fatalf("the same stamps: %v", err)
	}
	for _, fw := range b.servers[sc.Name] {
		if !kept(b.res, fw) {
			t.Errorf("the same stamps: %s was dropped\n%s", fw, log)
		}
	}
	// A new build of Tifa: Tifa and Tifa with its memo off are measured again, with their control, and
	// keep nothing of before. Next.js and Start keep everything, and so do the numbers all share.
	tifaBuilt := maps.Clone(stamps)
	tifaBuilt["tifa"], tifaBuilt["tifa-nocache"] = "t2", "t2"
	b, log, err = restore("shared", tifaBuilt, version)
	if err != nil {
		t.Fatalf("a new build of Tifa: %v", err)
	}
	for fw, want := range map[string]bool{"tifa": false, "tifa-nocache": false, "nextjs": true, "start": true} {
		if kept(b.res, fw) != want {
			t.Errorf("a new build of Tifa: %s kept %v, want %v\n%s", fw, !want, want, log)
		}
	}
	if len(b.res.Control) != 0 {
		t.Error("a new build of Tifa: the control of the old one is kept")
	}
	if b.res.ServerStamps["tifa"].Stamp != "t2" || b.res.ServerStamps["nextjs"].Stamp != "n1" {
		t.Errorf("the stamps kept: %+v", b.res.ServerStamps)
	}
	if b.res.Prepared[sc.Name] != 6*sc.blockVisits() || b.res.WarmVisits[sc.Name] != 3*sc.blockVisits() || b.res.LatencyRate[sc.Name] != 10 {
		t.Errorf("the numbers every server shares changed: prepared %v, warm-up %v, latency rate %v", b.res.Prepared, b.res.WarmVisits, b.res.LatencyRate)
	}
	if !strings.Contains(log, "measuring tifa again") || strings.Contains(log, "measuring nextjs again") {
		t.Errorf("the log does not say which servers are measured again:\n%s", log)
	}
	// Another engine alone (the stamps the same) is Tifa's too, and leaves Next.js and Start.
	engine := maps.Clone(version)
	engine[engineVersion] = "v0.32.0"
	b, log, err = restore("shared", stamps, engine)
	if err != nil {
		t.Fatalf("another engine: %v", err)
	}
	for fw, want := range map[string]bool{"tifa": false, "tifa-nocache": false, "nextjs": true, "start": true} {
		if kept(b.res, fw) != want {
			t.Errorf("another engine: %s kept %v, want %v\n%s", fw, !want, want, log)
		}
	}
	if b.res.Version[engineVersion] != "v0.32.0" {
		t.Errorf("the result keeps the old engine: %v", b.res.Version)
	}
	// Another Start: Start alone.
	startAgain := maps.Clone(version)
	startAgain["start"] = "1.170.0"
	b, _, err = restore("shared", stamps, startAgain)
	if err != nil {
		t.Fatalf("another Start: %v", err)
	}
	for fw, want := range map[string]bool{"tifa": true, "tifa-nocache": true, "nextjs": true, "start": false} {
		if kept(b.res, fw) != want {
			t.Errorf("another Start: %s kept %v, want %v", fw, !want, want)
		}
	}
	// A server the result does not know is measured, and the others kept.
	unknown := maps.Clone(stamps)
	b, _ = warmBrowse(t, sc, tifa, nocache, next, start)
	data := saved()
	res, _ := ReadBrowseResult(data)
	delete(res.ServerStamps, "start")
	data, _ = res.json()
	b.e, b.o.TraceDir, b.o.Stamp, b.o.ServerStamps, b.res = Env{Root: root}, dir, "shared", unknown, &BrowseResult{}
	if err := b.restore(data, cfg, version); err != nil {
		t.Fatal(err)
	}
	if kept(b.res, "start") || !kept(b.res, "nextjs") {
		t.Error("a server the result does not know is not measured, or the others are not kept")
	}
	// Another trace file of Next.js (the stamps the same, a run by hand): Next.js is measured again. The
	// latency rounds of every server were played at half of its rate, so they go too, and the closed
	// rounds of the others stay.
	os.WriteFile(filepath.Join(dir, "nextjs.jsonl"), []byte("recorded again"), 0o644)
	b, _, err = restore("shared", stamps, version)
	os.WriteFile(filepath.Join(dir, "nextjs.jsonl"), []byte("nextjs.jsonl"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.res.cellRounds("nextjs", sc.Name, 8)) != 0 || len(b.res.cellRounds("tifa", sc.Name, 8)) != 3 || len(b.res.Latency) != 0 || b.res.LatencyRate[sc.Name] != 0 {
		t.Errorf("another trace of Next.js: %d and %d closed rounds of Next.js and Tifa, %d latency rounds, rate %v", len(b.res.cellRounds("nextjs", sc.Name, 8)), len(b.res.cellRounds("tifa", sc.Name, 8)), len(b.res.Latency), b.res.LatencyRate)
	}
	// Every server measured again: nothing of the result is kept, so the numbers they share are worked
	// out again too.
	all := map[string]string{"tifa": "t2", "tifa-nocache": "t2", "nextjs": "n2", "start": "s2"}
	if b, _, err = restore("shared", all, version); err != nil || len(b.res.Prepared) != 0 || len(b.res.WarmVisits) != 0 {
		t.Errorf("every server measured again: prepared %v, warm-up %v (%v)", b.res.Prepared, b.res.WarmVisits, err)
	}
	// Another shared stamp is another run.
	if _, _, err := restore("other", stamps, version); err == nil || !strings.Contains(err.Error(), "remove it to start again") {
		t.Errorf("another shared stamp: %v", err)
	}
}

// When Next.js is measured again, the latency rounds of every server go with it: they were all played
// at half of the rate Next.js sustained.
func TestMeasuringNextJSAgainDropsTheLatencyOfEveryServer(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Sustain: map[string]*Sustain{sustainKey(sc.Name, "nextjs"): {Rate: 20, Done: true}}, LatencyRate: map[string]float64{sc.Name: 10}, LatencyBase: map[string]string{sc.Name: "nextjs"},
		Latency: []Round{{Framework: "tifa", Scenario: sc.Name}, {Framework: "nextjs", Scenario: sc.Name}}}
	what := r.forget("nextjs")
	if len(r.Latency) != 0 || r.LatencyRate[sc.Name] != 0 || r.LatencyBase[sc.Name] != "" || r.Sustain[sustainKey(sc.Name, "nextjs")] != nil {
		t.Errorf("after Next.js was dropped: %d latency rounds, rate %v, search %v", len(r.Latency), r.LatencyRate, r.Sustain)
	}
	if !strings.Contains(what, "latency") {
		t.Errorf("it does not say that the latency rounds of every server went: %q", what)
	}
}

// A result keeps, for each server, the stamp browse.sh gave it (its trace and its build), a hash of
// the trace file it replays and the date it was first measured under them.
func TestAResultKeepsTheStampOfEachServer(t *testing.T) {
	sc := mustScenario("shop")
	tifa, nocache, next := warmSpecs()
	b, _ := warmBrowse(t, sc, tifa, nocache, next)
	b.o.TraceDir = t.TempDir()
	os.WriteFile(TracePath(b.o.TraceDir, "tifa"), []byte("tifa"), 0o644)
	os.WriteFile(TracePath(b.o.TraceDir, "nextjs"), []byte("nextjs"), 0o644)
	b.o.Stamp, b.o.ServerStamps = "shared", map[string]string{"tifa": "t1", "tifa-nocache": "t2", "nextjs": "n1"}
	b.begin(map[string]string{"sessions": "2"}, map[string]string{"go": "go1.27"}, Machine{}, Placement{}, []string{"tifa", "nextjs"})
	if b.res.Stamp != "shared" {
		t.Errorf("the shared stamp is %q", b.res.Stamp)
	}
	for name, want := range map[string]string{"tifa": "t1", "tifa-nocache": "t2", "nextjs": "n1"} {
		got := b.res.ServerStamps[name]
		if got.Stamp != want || got.Trace != fileHash(TracePath(b.o.TraceDir, traceName(b.specs[name]))) || got.Date == "" {
			t.Errorf("%s: kept %+v, want the stamp %s, the hash of its trace and a date", name, got, want)
		}
	}
}

// The search of the sustainable rate starts within its cap, so a cap under 4 visits a second still
// measures, and halves below that.
func TestTheRateSearchStartsWithinTheCap(t *testing.T) {
	for _, tc := range []struct {
		max    float64
		passes float64 // the highest rate that passes
		tried  []float64
		lo, hi float64
	}{
		{2, 1, []float64{2, 1}, 1, 2},
		{3, 100, []float64{3}, 3, 0},
		{16384, 40, []float64{4, 8, 16, 32, 64}, 32, 64},
		{16384, 1, []float64{4, 2, 1}, 1, 2},
	} {
		var tried []float64
		lo, hi, limited, err := searchRate(tc.max, func(rate float64) (string, error) {
			tried = append(tried, rate)
			return map[bool]string{true: trialPass, false: trialFail}[rate <= tc.passes], nil
		})
		if err != nil || limited || lo != tc.lo || hi != tc.hi || !reflect.DeepEqual(tried, tc.tried) {
			t.Errorf("cap %v, passing up to %v: lo %v hi %v tried %v (%v), want %v %v %v", tc.max, tc.passes, lo, hi, tried, err, tc.lo, tc.hi, tc.tried)
		}
	}
	if _, err := (Env{}).Browse(context.Background(), BrowseOptions{MaxRate: 0.25}); err == nil || !strings.Contains(err.Error(), "-max-rate") {
		t.Errorf("a cap under half a visit a second: %v", err)
	}
}

// A scenario where Next.js has no sustainable rate in range has no open run: that is its result, and
// the latency phase goes on with the other scenarios.
func TestNoSustainableRateSkipsTheOpenRunOfThatScenarioOnly(t *testing.T) {
	sc, _ := BrowseScenarioByName("shop")
	b := &browse{o: BrowseOptions{Log: io.Discard, Rounds: 3}, servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, res: &BrowseResult{
		Sustain:     map[string]*Sustain{sustainKey(sc.Name, openBase): {Done: true, Note: "no rate passed its confirmation rounds"}},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{},
	}}
	if err := b.latency(context.Background(), sc); err != nil {
		t.Fatalf("the latency phase stopped: %v", err)
	}
	if len(b.res.Latency) != 0 {
		t.Fatalf("open rounds were run: %d", len(b.res.Latency))
	}
	r := b.res
	r.Scenarios, r.Servers, r.Version = []BrowseScenario{sc}, b.servers, map[string]string{}
	if md := r.Markdown(); !strings.Contains(md, "No open run: Next.js has no sustainable rate in range on this scenario (no rate passed its confirmation rounds)") {
		t.Errorf("the report does not say why there is no open run:\n%s", md)
	}
	// Without a search done it is still an error: the sustain phase did not run.
	b.res.Sustain = map[string]*Sustain{}
	if err := b.latency(context.Background(), sc); err == nil {
		t.Error("no search and no error")
	}
}

// A closed round outside the length window resizes its cell from what it did, a round stopped at
// twice the target too: it played only part of its blocks, and the next round must not get the same
// blocks again.
func TestARoundOutsideTheWindowResizesItsCell(t *testing.T) {
	target := 75 * time.Second
	for _, tc := range []struct {
		name string
		out  Round
		want bool
	}{
		{"stopped at twice the target", Round{Seconds: 150, Visits: 3555, TimedOut: true}, true},
		{"too short", Round{Seconds: 40, Visits: 3800}, true},
		{"in the window", Round{Seconds: 90, Visits: 3800}, false},
		{"nothing ended", Round{Seconds: 150, TimedOut: true}, false},
	} {
		tc.out.Pass, tc.out.Valid = true, true
		blocks := calibrate(&tc.out, target, 4, 95)
		if (blocks > 0) != tc.want || tc.out.Valid == tc.want {
			t.Errorf("%s: %d blocks, valid %v", tc.name, blocks, tc.out.Valid)
		}
		if want := BlocksFor(float64(tc.out.Visits)/tc.out.Seconds, target, 4, 95); tc.want && blocks != want {
			t.Errorf("%s: %d blocks, want %d", tc.name, blocks, want)
		}
	}
}

// A cell with no valid round says why its rounds failed, not only how many there were.
func TestACellWithNoValidRoundSaysWhyItsRoundsFailed(t *testing.T) {
	sc, _ := BrowseScenarioByName("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}}
	for round := 1; round <= 3; round++ {
		r.Closed = append(r.Closed, closedRound("nextjs", sc.Name, 32, round, 400, 10, true))
		bad := closedRound("tifa", sc.Name, 32, 1, 4000, 59, false)
		bad.Valid, bad.Attempt = false, round
		bad.Why, bad.WhyInvalid = "630 failures (GET /zh-CN/store/cart: status 503)", "the round took 59 s, outside 60 to 120 s: resized"
		r.Closed = append(r.Closed, bad)
	}
	md := r.Markdown()
	if want := "not decided (no valid round of 3: 630 failures (GET /zh-CN/store/cart: status 503); the round took 59 s, outside 60 to 120 s: resized) |"; !strings.Contains(md, want) {
		t.Errorf("the report lacks %q:\n%s", want, md)
	}
}

// A result of the old format, which counted users and had no planned visits, is refused with a clear
// error, by the report and by a run that would continue it: there is no reading of it.
func TestAResultOfTheOldFormatIsRefused(t *testing.T) {
	cur := &BrowseResult{Config: browseConfig(BrowseOptions{Sessions: 8}, "", "", ""), Closed: []Round{{VisitsPlanned: 4}}, Sustain: map[string]*Sustain{}}
	data, err := cur.json()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBrowseResult(data); err != nil {
		t.Fatalf("the current format: %v", err)
	}
	for name, old := range map[string]string{
		"users in the settings": `{"settings":{"users":"32","rounds":"3"},"closed":[],"sustain":{}}`,
		"users in a round":      `{"settings":{"sessions":"8"},"closed":[{"users":32,"visits":10}],"sustain":{}}`,
		"a saturation file":     `{"settings":{"sessions":"8"},"closed":[],"sustain":{},"saturated":{}}`,
		"an old latency round":  `{"settings":{"sessions":"8"},"closed":[],"latency":[{"visits":10}],"sustain":{}}`,
		"an old control round":  `{"settings":{"sessions":"8"},"closed":[],"control":[{"users":32,"visitsPlanned":4}],"sustain":{}}`,
		"an old trial":          `{"settings":{"sessions":"8"},"closed":[],"sustain":{"shop/nextjs":{"trials":[{"visits":10}],"confirm":[]}}}`,
		"an old confirmation":   `{"settings":{"sessions":"8"},"closed":[],"sustain":{"shop/nextjs":{"trials":[],"confirm":[{"visits":10}]}}}`,
	} {
		if _, err := ReadBrowseResult([]byte(old)); err == nil || !strings.Contains(err.Error(), "older version") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
