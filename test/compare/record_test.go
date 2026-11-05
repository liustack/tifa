package compare

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

// mkRun builds a run of a spec with the given requests per second and P50, and fills the rest from
// them so that the other metrics have values.
func mkRun(spec, framework string, round int, req, p50 float64, configs ...string) Run {
	return Run{
		Spec: spec, Framework: framework, Label: spec, Configs: configs, Round: round,
		Metrics: Metrics{
			Requests: req, P50: p50, P99: p50 * 2, Max: p50 * 4, FirstAnswer: 300, First100: p50 * 3, First1000: p50 * 1.5,
			Settle: 40, RSSStart: 50, RSSPeak: 60, HitRate: 0,
		},
	}
}

func fixture() *Record {
	r := &Record{
		Date:      "2026-11-06 10:00:00 UTC",
		Versions:  map[string]string{"go": "go1.27.2", "quickjs": "v0.30.1", "node": "v24.13.0", "nextjs": "16.4.2", "start": "1.169.3"},
		Machine:   Machine{CPU: "Test CPU", Kernel: "Linux 6.8", OS: "linux", Arch: "amd64"},
		Placement: Placement{Server: "1", Load: "2", Checked: true, Note: "server and load are on different physical cores"},
		Settings:  Settings{Rounds: 2, Steady: "30s", Cold: 1000},
	}
	def, eng := []string{"default"}, []string{"engine"}
	both := []string{"default", "engine"}
	for round, req := range []float64{28.8, 29.0} {
		tifa := mkRun("tifa", "tifa", round+1, req*5, 6, def...)
		tifa.Label, tifa.Renders = "Tifa", map[string]int{"vm": 6, "cache": 4994}
		tifa.Metrics.HitRate = 0.9988
		nocache := mkRun("tifa-nocache", "tifa", round+1, req, 30, eng...)
		nocache.Label, nocache.Renders = "Tifa (cache off)", map[string]int{"vm": 5000}
		next := mkRun("nextjs", "nextjs", round+1, 81+float64(round), 10, both...)
		next.Label = "Next.js"
		jitless := mkRun("nextjs-jitless", "nextjs", round+1, 25, 35, eng...)
		jitless.Label, jitless.Reference = "Next.js --jitless", true
		r.Runs = append(r.Runs, tifa, nocache, next, jitless)
		for _, id := range []string{"tifa-aa1", "tifa-aa2"} {
			aa := mkRun(id, "tifa", round+1, 100+float64(round), 5, "aa")
			aa.AA = true
			r.Runs = append(r.Runs, aa)
		}
	}
	r.AA = AASpread(r.Runs)
	return r
}

func roundTrip(t *testing.T, r *Record) *Record {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Record
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	return &back
}

func TestMarkdownTables(t *testing.T) {
	md := roundTrip(t, fixture()).Markdown()
	for _, want := range []string{
		"### Out of the box",
		"### Engine",
		"### Memory",
		"Test CPU", "Go 1.27.2", "quickjs v0.30.1", "different physical cores",
		// the median and the range of the rounds
		"| Tifa | 144.5 (144.0 to 145.0)",
		"| Tifa (cache off) | 28.9 (28.8 to 29.0)",
		"| Next.js --jitless (reference) |",
		"99.88%", // hit rate of the default Tifa, 0.9988
		"A/A", "requests/s",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	section := func(from, to string) string {
		i := strings.Index(md, from)
		j := strings.Index(md, to)
		if i < 0 || j < i {
			t.Fatalf("sections %q, %q out of order in\n%s", from, to, md)
		}
		return md[i:j]
	}
	if def := section("### Out of the box", "### Engine"); strings.Contains(def, "jitless") || strings.Contains(def, "28.9") {
		t.Errorf("the default table holds an engine-only row:\n%s", def)
	}
	if eng := section("### Engine", "### Memory"); !strings.Contains(eng, "Next.js |") {
		t.Errorf("the engine table lacks Next.js:\n%s", eng)
	}
}

func TestMarkdownIsEmptyTablesWhenNothingRan(t *testing.T) {
	r := fixture()
	r.Runs = nil
	if md := r.Markdown(); !strings.Contains(md, "### Out of the box") {
		t.Errorf("no headings for an empty record:\n%s", md)
	}
}

// scaled returns the fixture with one metric of the default Tifa multiplied.
func scaled(f func(*Metrics) *float64, by float64) *Record {
	r := fixture()
	for i := range r.Runs {
		if r.Runs[i].Spec == "tifa" {
			*f(&r.Runs[i].Metrics) *= by
		}
	}
	return r
}

func TestDiffThresholds(t *testing.T) {
	req := func(m *Metrics) *float64 { return &m.Requests }
	p50 := func(m *Metrics) *float64 { return &m.P50 }
	hit := func(m *Metrics) *float64 { return &m.HitRate }
	cases := []struct {
		name    string
		new     *Record
		spread  float64 // A/A spread of the new record, for all metrics
		flagged int
	}{
		{"same", fixture(), 0, 0},
		{"2.9% fewer requests is over 2%", scaled(req, 0.971), 0, 1},
		{"1.9% fewer requests is under 2%", scaled(req, 0.981), 0, 0},
		{"A/A spread of 5% widens the limit", scaled(req, 0.96), 0.05, 0},
		{"faster is never flagged", scaled(req, 1.5), 0, 0},
		{"slower P50 is flagged", scaled(p50, 1.05), 0, 1},
		{"lower hit rate is flagged", scaled(hit, 0.9), 0, 1},
	}
	for _, c := range cases {
		old := fixture()
		old.AA = nil
		c.new.AA = map[string]float64{}
		for _, d := range metricDefs {
			c.new.AA[d.Key] = c.spread
		}
		lines, flagged := DiffRecords(old, c.new)
		if flagged != c.flagged {
			t.Errorf("%s: %d flagged, want %d:\n%s", c.name, flagged, c.flagged, strings.Join(lines, "\n"))
		}
	}
}

func TestDiffLooksOnlyAtTifaAndNamesTheMetric(t *testing.T) {
	r := fixture()
	for i := range r.Runs {
		if r.Runs[i].Framework == "nextjs" {
			r.Runs[i].Metrics.Requests /= 3
		}
	}
	if lines, flagged := DiffRecords(fixture(), r); flagged != 0 {
		t.Errorf("a Node framework was flagged:\n%s", strings.Join(lines, "\n"))
	}
	lines, flagged := DiffRecords(fixture(), scaled(func(m *Metrics) *float64 { return &m.Requests }, 0.9))
	text := strings.Join(lines, "\n")
	if flagged != 1 || !strings.Contains(text, "tifa") || !strings.Contains(text, "requests/s") {
		t.Errorf("flagged %d:\n%s", flagged, text)
	}
	// The A/A runs of the new record are not compared with anything.
	bad := fixture()
	for i := range bad.Runs {
		if bad.Runs[i].AA {
			bad.Runs[i].Metrics.Requests = 1
		}
	}
	if _, flagged := DiffRecords(fixture(), bad); flagged != 0 {
		t.Errorf("A/A runs were compared: %d flagged", flagged)
	}
}
