package compare

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func under() map[string]string {
	return map[string]string{"scenario": "a1", "plans": "b2", "trace": "c3", "sources": "d4", "cpu": "AMD EPYC 7B13", "serverCpus": "0", "loadCpus": "1,5",
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
	for _, name := range []string{"plans-store.json", "nextjs-store.jsonl", "STAMP-store"} {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
	}
	sc := mustScenario("store-w5")
	var log strings.Builder
	b := &browse{o: BrowseOptions{Log: &log, Rounds: 3, TraceDir: dir, ServerCPUs: "0", LoadCPUs: "1,5", TrialWindow: 45e9, RateFile: filepath.Join(dir, "rates.json")},
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
	b.res.Sustain = map[string]*Sustain{}
	b.res.Version["nextjs"] = "16.0.2"
	if err := b.sustain(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "nextjs") || !strings.Contains(err.Error(), "-probe") {
		t.Errorf("another Next.js: %v", err)
	}
}
