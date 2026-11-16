package compare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// browse.sh status says, for each server, whether the result holds results a run with the stamps of
// this checkout keeps (current), measures again (stale, with why) or has none (missing), and how many
// rounds of each part it has out of how many: a reference plays one closed round a cell. It changes
// nothing.
func TestTheStatusSaysWhatARunKeepsOfEachServer(t *testing.T) {
	sc := mustScenario("shop")
	dir := t.TempDir()
	for _, name := range []string{"tifa.jsonl", "nextjs.jsonl", "start.jsonl"} {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
	}
	trace := func(name string) string { return fileHash(TracePath(dir, name)) }
	res := &BrowseResult{Stamp: "shared", Scenarios: []BrowseScenario{sc}, Config: map[string]string{"rounds": "3", "sessions": "8"}, Version: map[string]string{},
		ServerStamps: map[string]ServerStamp{
			"tifa":         {Stamp: "t1", Trace: trace("tifa"), Date: "2026-11-16 07:00:00 UTC"},
			"tifa-nocache": {Stamp: "t1", Trace: trace("tifa"), Date: "2026-11-16 07:00:00 UTC"},
			"nextjs":       {Stamp: "n1", Trace: trace("nextjs"), Date: "2026-11-15 07:00:00 UTC"},
		},
		Throughput: map[string]*Throughput{"shop/nextjs": {Result: ThroughputCoreUsed, Sessions: 13}}, LatencyRate: map[string]float64{sc.Name: 3}}
	for round := 1; round <= 2; round++ {
		res.Closed = append(res.Closed, closedRound("tifa", sc.Name, 8, round, 4000, 100, true))
	}
	for round := 1; round <= 3; round++ {
		res.Closed = append(res.Closed, closedRound("nextjs", sc.Name, 8, round, 400, 100, true))
	}
	res.Latency = append(res.Latency, Round{Framework: "nextjs", Scenario: sc.Name, Round: 1, Valid: true})
	res.Closed = append(res.Closed, closedRound("tifa-nocache", sc.Name, 8, 1, 3000, 100, true))
	res.Control = []Round{{Framework: "tifa", Scenario: sc.Name, Round: 1, Valid: true}}
	data, err := res.json()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "browse.json")
	os.WriteFile(path, data, 0o644)
	stamps := map[string]string{"tifa": "t1", "tifa-nocache": "t1", "nextjs": "n2", "start": "s1"}
	for server, wants := range map[string][]string{
		"tifa":         {"result: current, measured 2026-11-16 07:00:00 UTC", "throughput: 2 of 3 rounds at 8 sessions, steps not done", "control: 1 of 1 round", "latency: 0 of 1 round"},
		"tifa-nocache": {"result: current", "throughput: 1 of 1 round at 8 sessions"},
		"nextjs":       {"result: stale, a run measures it again: its stamp changed", "throughput: 3 of 3 rounds at 8 sessions, steps done (core used at 13 sessions)", "latency: 1 of 1 round, at 3.00 visits/s"},
		"start":        {"result: missing: the result does not have it"},
	} {
		got, err := BrowseStatus(path, dir, "shared", stamps, server)
		if err != nil {
			t.Fatalf("%s: %v", server, err)
		}
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the status lacks %q:\n%s", server, want, got)
			}
		}
	}
	if got, _ := BrowseStatus(path, dir, "other", stamps, "tifa"); !strings.Contains(got, "result: stale, a run refuses the whole file") {
		t.Errorf("another shared stamp:\n%s", got)
	}
	if got, err := BrowseStatus(filepath.Join(t.TempDir(), "none.json"), dir, "shared", stamps, "tifa"); err != nil || !strings.Contains(got, "result: missing: no ") {
		t.Errorf("no result file: %v\n%s", err, got)
	}
	if after, _ := os.ReadFile(path); string(after) != string(data) {
		t.Error("the status changed the result")
	}
}

// The status gives the base of the calibrations of a result once, or says that a run sets it.
func TestTheStatusGivesTheCalibrationBase(t *testing.T) {
	dir := t.TempDir()
	res := &BrowseResult{Calibration: &CalibrationBase{Compute: 436.9, Memory: 6.05, Runs: make([]Calibration, 5), Date: "2026-11-16 12:00:00 UTC"}}
	write := func() string {
		data, err := res.json()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "browse.json")
		os.WriteFile(path, data, 0o644)
		return path
	}
	if got, err := BrowseCalibrationStatus(write()); err != nil || got != "calibration: base compute 436.9 M/s, memory 6.05 M/s, the medians of 5 calibrations at 2026-11-16 12:00:00 UTC\n" {
		t.Errorf("%q (%v)", got, err)
	}
	res.Calibration = nil
	if got, _ := BrowseCalibrationStatus(write()); got != "calibration: no base yet, a run sets it before its first round\n" {
		t.Errorf("%q", got)
	}
	if got, err := BrowseCalibrationStatus(filepath.Join(dir, "none.json")); err != nil || got != "" {
		t.Errorf("no result file: %q (%v)", got, err)
	}
}
