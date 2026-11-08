package compare

import (
	"encoding/json/v2"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func under() map[string]string {
	return map[string]string{"method": "m1", "nextjsApp": "n1", "scenario": "a1", "plans": "b2", "trace": "c3", "sources": "d4", "cpu": "AMD EPYC 7B13", "serverCpus": "0", "loadCpus": "1,5",
		"node": "v24.1.0", "nextjs": "16.0.1", "actionSpacing": "1s", "rule": LatencyRule, "search": "trial 45s", "confirm": "3 rounds"}
}

// A rate in the file is used only when every condition it was measured under is that of the run.
// On a mismatch the run is refused and asked for -probe: the rate is never reused silently. With
// -probe, or no entry for the scenario, the rate is searched.
func TestTheRateFileIsUsedOnlyUnderTheSameConditions(t *testing.T) {
	file := RateFile{"store-w5": {Rate: 21.5, Unit: "visits/s", Under: under(), Source: RateSource{Date: "2026-11-09", Tifa: "abc1234"}}}
	if e, err := fileRate(file, "store-w5", under(), false); err != nil || e == nil || e.Rate != 21.5 {
		t.Fatalf("the same conditions: %+v %v", e, err)
	}
	if e, err := fileRate(file, "admin-w20", under(), false); err != nil || e != nil {
		t.Errorf("no entry: %+v %v, want a search", e, err)
	}
	if e, err := fileRate(file, "store-w5", under(), true); err != nil || e != nil {
		t.Errorf("-probe: %+v %v, want a search", e, err)
	}
	for _, k := range []string{"cpu", "trace", "nextjs", "sources", "rule"} {
		u := under()
		u[k] = "other"
		e, err := fileRate(file, "store-w5", u, false)
		if e != nil || err == nil || !strings.Contains(err.Error(), "-probe") || !strings.Contains(err.Error(), k) {
			t.Errorf("%s differs: %+v %v", k, e, err)
		}
	}
	// A condition the run cannot tell (traces recorded by hand, with no stamp) never matches.
	u, f := under(), RateFile{"store-w5": file["store-w5"]}
	u["sources"] = ""
	f["store-w5"].Under["sources"] = ""
	if e, err := fileRate(f, "store-w5", u, false); e != nil || err == nil {
		t.Errorf("an unknown condition: %+v %v", e, err)
	}
}

// The file keeps an entry per scenario, and writing one replaces only itself.
func TestTheRateFileKeepsEachScenario(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rates.json")
	f, err := readRates(path)
	if err != nil || len(f) != 0 {
		t.Fatalf("a missing file: %v %v", f, err)
	}
	a := RateEntry{Rate: 21.5, Unit: "visits/s", Under: under(), Source: RateSource{Date: "2026-11-09", Tifa: "abc1234", Confirm: []RateRound{{Rate: 21.5, Round: 1, Attempt: 1, Pass: true, Valid: true}}}}
	b := a
	b.Rate = 30
	for _, e := range []struct {
		sc string
		e  RateEntry
	}{{"store-w5", a}, {"admin-w20", a}, {"admin-w20", b}} {
		if err := writeRate(path, e.sc, e.e); err != nil {
			t.Fatal(err)
		}
	}
	f, err = readRates(path)
	if err != nil || !reflect.DeepEqual(f, RateFile{"store-w5": a, "admin-w20": b}) {
		t.Errorf("file %+v (%v)", f, err)
	}
	os.WriteFile(path, []byte(`{"store-w5": {"users": 32}}`), 0o644)
	if _, err := readRates(path); err == nil {
		t.Error("a file of another format is read")
	}
}

// The report says where a rate read from the file was confirmed.
func TestTheReportSaysWhereARateFromTheFileWasConfirmed(t *testing.T) {
	sc := mustScenario("admin-w20")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{
		"admin-w20/nextjs": {Rate: 20, Done: true, From: &RateSource{Date: "2026-11-09 10:00:00 UTC", Tifa: "abc1234", Result: "browse.json", Confirm: []RateRound{{Pass: true, Valid: true}, {Pass: true, Valid: true}, {Pass: true, Valid: true}}}}}}
	if md := r.Markdown(); !strings.Contains(md, "| Next.js | 20.00 | 80 | from test/compare/rates.json: confirmed on 2026-11-09 10:00:00 UTC with Tifa abc1234 (browse.json), 3 of 3 confirmation rounds passed |") {
		t.Errorf("the report:\n%s", md)
	}
}

// The sustain phase reads a rate of the file when the run is under its conditions, searches nothing,
// and refuses a run under others.
func TestTheSustainPhaseReadsTheFileOrRefuses(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"plans-store.json", "nextjs-store.jsonl"} {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "STAMP-store"), []byte("tree js abc\nsite store\n"), 0o644)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sc := mustScenario("store-w5")
	var log strings.Builder
	b := &browse{e: Env{Root: root, Next: builtNext(t, root)}, o: BrowseOptions{Log: &log, Rounds: 3, TraceDir: dir, Stamp: "from browse.sh", ServerCPUs: "0", LoadCPUs: "1,5", TrialWindow: 45e9, RateFile: filepath.Join(dir, "rates.json")},
		servers: map[string][]string{sc.Name: {"tifa", "nextjs"}},
		res:     &BrowseResult{Machine: Machine{CPU: "AMD EPYC 7B13"}, Version: map[string]string{"node": "v24.1.0", "nextjs": "16.0.1"}, Sustain: map[string]*Sustain{}}}
	if err := writeRate(b.o.RateFile, sc.Name, RateEntry{Rate: 21.5, Unit: rateUnit, Under: b.rateConditions(sc), Source: RateSource{Date: "2026-11-09"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.sustain(t.Context(), sc); err != nil {
		t.Fatal(err)
	}
	if s := b.res.Sustain["store-w5/nextjs"]; s == nil || s.Rate != 21.5 || !s.Done || s.From == nil || len(s.Trials) != 0 {
		t.Errorf("the rate from the file: %+v", s)
	}
	// A run by hand cannot tell the sources of its traces: it is refused too.
	b.res.Sustain = map[string]*Sustain{}
	b.o.Stamp = ""
	if err := b.sustain(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "sources") {
		t.Errorf("a run by hand: %v", err)
	}
	b.o.Stamp = "from browse.sh"
	b.res.Sustain = map[string]*Sustain{}
	b.res.Version["nextjs"] = "16.0.2"
	if err := b.sustain(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "nextjs") || !strings.Contains(err.Error(), "-probe") {
		t.Errorf("another Next.js: %v", err)
	}
}

// A rate that the load generator limited is never written into the file: it could not be reused.
func TestALimitedRateIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rates.json")
	b := &browse{o: BrowseOptions{RateFile: path, Log: &strings.Builder{}}, res: &BrowseResult{}}
	if err := b.keepRate(mustScenario("store-w5"), &Sustain{Rate: 20, Limited: true, Done: true, Under: under()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a limited rate was written")
	}
}

// The method of a rate is a hash of the code that shapes the load and the verdicts, and the app a
// hash of the sources of the Next.js app and the domain code it shares (the files git tracks, as they
// are on disk): a change to either is another condition. Caches and installed packages are not the app. A file that is missing makes the condition unknown.
func TestTheRateIsBoundToTheMethodAndTheApp(t *testing.T) {
	root := t.TempDir()
	write := func(name, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755)
		os.WriteFile(filepath.Join(root, name), []byte(s), 0o644)
	}
	for _, f := range methodFiles {
		write(filepath.Join("test", "compare", f), f)
	}
	write("test/compare/nextjs/src/page.tsx", "a")
	write("test/compare/nextjs/package.json", "{}")
	write("test/compare/domain/orders.ts", "o")
	for _, args := range [][]string{{"init", "-q"}, {"add", "test/compare/nextjs", "test/compare/domain"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	method, app := methodHash(root), appHash(root)
	if method == "" || app == "" {
		t.Fatalf("method %q, app %q", method, app)
	}
	write("test/compare/nextjs/node_modules/x/index.js", "x")
	write("test/compare/nextjs/.next/cache/y", "y")
	if appHash(root) != app {
		t.Error("the installed packages or the build cache change the app")
	}
	write("test/compare/nextjs/src/page.tsx", "b")
	if appHash(root) == app {
		t.Error("a change to the app is the same app")
	}
	write("test/compare/domain/orders.ts", "changed")
	if appHash(root) == app {
		t.Error("a change to the shared domain code is the same app")
	}
	write("test/compare/replay.go", "changed")
	if methodHash(root) == method {
		t.Error("a change to the replay is the same method")
	}
	changed := methodHash(root)
	write("test/compare/stats.go", "another percentile")
	if methodHash(root) == changed {
		t.Error("a change to the percentiles is the same method")
	}
	os.Remove(filepath.Join(root, "test", "compare", "load.go"))
	if methodHash(root) != "" {
		t.Error("a missing file of the method gives a method")
	}
	for _, f := range []string{"load.go", "replay.go", "stats.go", "store_writes.go", "writepool.go", "browse.go", "throughput.go"} {
		if !slices.Contains(methodFiles, f) {
			t.Errorf("the method leaves out %s", f)
		}
	}
}

// The sources of the traces are known only from a stamp that says what they were recorded from (a
// site and its trees, nothing uncommitted), and only in a run that browse.sh checked against this
// checkout (one with a stamp): a run by hand cannot tell them, so it never reuses a rate.
func TestTheSourcesOfTheTracesMustBeKnown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "STAMP-store")
	good := "tree js abc\ntree examples/shop def\nsite store\nplans 123\n"
	for _, tc := range []struct {
		name, content string
		known         bool
	}{{"a good stamp", good, true}, {"an empty stamp", "", false}, {"another site", strings.Replace(good, "site store", "site workbench", 1), false},
		{"no trees", "site store\n", false}, {"uncommitted sources", good + "dirty\n", false}} {
		os.WriteFile(path, []byte(tc.content), 0o644)
		if got := stampHash(path, SiteStore); (got != "") != tc.known {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
	os.WriteFile(path, []byte(good), 0o644)
	sc := mustScenario("store-w5")
	b := &browse{o: BrowseOptions{TraceDir: dir}, res: &BrowseResult{Version: map[string]string{}}}
	if b.rateConditions(sc)["sources"] != "" {
		t.Error("a run by hand knows the sources of its traces")
	}
	b.o.Stamp = "from browse.sh"
	if b.rateConditions(sc)["sources"] == "" {
		t.Error("a run of browse.sh does not know the sources of its traces")
	}
	if got := slices.Sorted(maps.Keys(b.rateConditions(sc))); !slices.Equal(got, rateKeys) {
		t.Errorf("the conditions %v, the keys the file is checked for %v", got, rateKeys)
	}
}

// The file is checked when it is read: the unit is visits/s, the rate is above 0, and every
// condition is there. It is written whole or not at all, through a file it then renames.
func TestTheRateFileIsCheckedWhenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rates.json")
	good := RateEntry{Rate: 21.5, Unit: rateUnit, Under: under(), Source: RateSource{Date: "2026-11-09"}}
	if err := writeRate(path, "store-w5", good); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("the write left %d files", len(entries))
	}
	for name, mod := range map[string]func(*RateEntry){
		"another unit":     func(e *RateEntry) { e.Unit = "pageviews/s" },
		"no unit":          func(e *RateEntry) { e.Unit = "" },
		"a rate of 0":      func(e *RateEntry) { e.Rate = 0 },
		"a missing method": func(e *RateEntry) { delete(e.Under, "method") },
		"an empty sources": func(e *RateEntry) { e.Under["sources"] = "" },
	} {
		e := good
		e.Under = maps.Clone(good.Under)
		mod(&e)
		data, _ := json.Marshal(RateFile{"store-w5": e})
		os.WriteFile(path, data, 0o644)
		if _, err := readRates(path); err == nil {
			t.Errorf("%s: the file is read", name)
		}
	}
}

// A rate that a result already holds is checked again when the result is continued: under other
// conditions (another Next.js, another method) the run is refused, whether the rate was searched or
// read from the file.
func TestAContinuedRateIsCheckedAgain(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"plans-store.json", "nextjs-store.jsonl"} {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "STAMP-store"), []byte("tree js abc\nsite store\n"), 0o644)
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	sc := mustScenario("store-w5")
	b := &browse{e: Env{Root: root, Next: builtNext(t, root)}, o: BrowseOptions{Log: io.Discard, Rounds: 3, TraceDir: dir, Stamp: "s", ServerCPUs: "0", LoadCPUs: "1", TrialWindow: 45e9, RateFile: filepath.Join(dir, "rates.json")},
		servers: map[string][]string{sc.Name: {"tifa", "nextjs"}},
		res:     &BrowseResult{Machine: Machine{CPU: "AMD EPYC 7B13"}, Version: map[string]string{"node": "v24.1.0", "nextjs": "16.0.1"}, Sustain: map[string]*Sustain{}}}
	b.res.Sustain["store-w5/nextjs"] = &Sustain{Rate: 20, Done: true, Under: b.rateConditions(sc)}
	if err := b.sustain(t.Context(), sc); err != nil {
		t.Fatalf("the same conditions: %v", err)
	}
	b.res.Version["nextjs"] = "16.0.2"
	if err := b.sustain(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "nextjs") {
		t.Errorf("another Next.js: %v", err)
	}
}

// The build of the Next.js app records the sources it was made from (appHash) in its output, and a
// rate is reused or searched only on a build of the sources of the checkout: a stale build, or one
// that says nothing, is refused. The cache that Next.js writes while it serves changes nothing.
func TestARateIsOnlyUsedOnABuildOfTheSources(t *testing.T) {
	root := t.TempDir()
	next := filepath.Join(root, "test", "compare", "nextjs")
	write := func(name, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755)
		os.WriteFile(filepath.Join(root, name), []byte(s), 0o644)
	}
	write("test/compare/nextjs/src/page.tsx", "a")
	write("test/compare/domain/orders.ts", "o")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	b := &browse{e: Env{Root: root, Next: next}}
	if err := b.checkNextBuild(); err == nil || !strings.Contains(err.Error(), "build") {
		t.Errorf("a build that says nothing: %v", err)
	}
	os.MkdirAll(filepath.Join(next, ".next"), 0o755)
	if err := writeNextSource(root, next); err != nil {
		t.Fatal(err)
	}
	if err := b.checkNextBuild(); err != nil {
		t.Errorf("a build of the sources: %v", err)
	}
	write("test/compare/nextjs/.next/cache/fetch", "x")
	if err := b.checkNextBuild(); err != nil {
		t.Errorf("the cache of the build: %v", err)
	}
	write("test/compare/nextjs/src/page.tsx", "b")
	if err := b.checkNextBuild(); err == nil || !strings.Contains(err.Error(), "other sources") {
		t.Errorf("a stale build: %v", err)
	}
	writeNextSource(root, next)
	write("test/compare/nextjs/shop/app/page.tsx", "the pages copied from the shop")
	if err := b.checkNextBuild(); err == nil {
		t.Error("pages copied from the shop after the build are the sources it was built from")
	}
}

// builtNext is a Next.js app folder whose build says it was made from the sources at root.
func builtNext(t *testing.T, root string) string {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".next"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, ".next", nextSourceFile), []byte(appHash(root)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
