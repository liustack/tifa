package compare

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

// mkRun builds a run of a spec with the given requests per second and P50, and fills the rest from
// them so that the other metrics have values.
func mkRun(spec, framework string, round int, req, p50 float64, configs ...string) Run {
	return Run{
		Spec: spec, Framework: framework, Label: spec, Configs: configs, Round: round, Scenario: ScenarioRead,
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
		tifa.Label, tifa.Renders = "Tifa", map[string]int{"vm": 6, "memo": 4994}
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

func TestPageStats(t *testing.T) {
	// two pages in turn: page 0 takes 10 and 20 ms, page 1 takes 40 ms
	lat := []float64{10, 40, 20, 40, 10, 40}
	got := pageStats(lat, []int{0, 1, 0, 1, 0, 1})
	if len(got) != 2 || got[0].Page != Pages[0].Name || got[1].Page != Pages[1].Name {
		t.Fatalf("pages: %+v", got)
	}
	if got[0].P50 != 10 || got[1].P50 != 40 {
		t.Errorf("P50: %+v", got)
	}
	// one request in flight: requests a second is 1000 over the mean latency
	if want := 1000 / (40.0 / 3); got[0].Requests < want-0.01 || got[0].Requests > want+0.01 {
		t.Errorf("page 0 requests/s = %v, want %v", got[0].Requests, want)
	}
	if got[1].Requests != 25 {
		t.Errorf("page 1 requests/s = %v, want 25", got[1].Requests)
	}
}

func withPages(r *Record) *Record {
	for i := range r.Runs {
		m := r.Runs[i].Metrics
		for _, p := range Pages {
			r.Runs[i].Pages = append(r.Runs[i].Pages, PageStat{Page: p.Name, P50: m.P50, Requests: m.Requests})
		}
	}
	for _, p := range Pages {
		r.Render = append(r.Render, RenderResult{Page: p.Name, P50: 25, P99: 30})
	}
	return r
}

func TestMarkdownPerPageAndRender(t *testing.T) {
	md := roundTrip(t, withPages(fixture())).Markdown()
	for _, want := range []string{
		"### Per page",
		"| list studio |",
		"81.5 (10.00 ms), 2.82x", // Next.js against Tifa with the cache off, in the engine table
		"### Render and the rest",
		"| list studio | 30.00 | 25.00 | 5.00 | 17% |",
		"| mean of the six |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if plain := fixture().Markdown(); strings.Contains(plain, "### Per page") || strings.Contains(plain, "### Render and the rest") {
		t.Errorf("a record without pages or render prints their tables:\n%s", plain)
	}
}

func TestParseRender(t *testing.T) {
	var out strings.Builder
	for i, p := range Pages {
		fmt.Fprintf(&out, "BenchmarkShop/render%s-8   2000   1000 ns/op   %d.5 p50-ms   %d.9 p99-ms\n", strings.ReplaceAll(p.URL, "/", "-"), i+10, i+20)
	}
	got, err := parseRender("goos: linux\n" + out.String() + "PASS\n")
	if err != nil || len(got) != len(Pages) || got[2].P50 != 12.5 || got[2].P99 != 22.9 || got[2].Page != Pages[2].Name {
		t.Errorf("parsed %+v, %v", got, err)
	}
	if _, err := parseRender("nothing"); err == nil {
		t.Error("output with no page was accepted")
	}
}

// withWrites adds the runs of the two scenarios with writes: Tifa and Next.js with the cache on
// and 90% hits in the default configuration, and Tifa alone with and without its cache when every
// read follows a write.
func withWrites(r *Record) *Record {
	for round := 1; round <= 2; round++ {
		tifa := mkRun("tifa", "tifa", round, 120, 7, ConfigDefault)
		tifa.Label, tifa.Scenario = "Tifa", ScenarioReadWrite
		tifa.Renders, tifa.Metrics.HitRate = map[string]int{"vm": 500, "memo": 4500}, 0.9
		tifa.Metrics.Writes, tifa.Metrics.WriteP50, tifa.Metrics.WriteP99 = 500, 3.5, 9.5
		next := mkRun("nextjs", "nextjs", round, 80, 10, ConfigDefault, ConfigEngine)
		next.Label, next.Scenario = "Next.js", ScenarioReadWrite
		hit := mkRun("tifa", "tifa", round, 30, 31, ConfigDefault)
		hit.Label, hit.Scenario, hit.Renders = "Tifa", ScenarioWriteEach, map[string]int{"vm": 5000}
		miss := mkRun("tifa-nocache", "tifa", round, 31, 30, ConfigEngine)
		miss.Label, miss.Scenario, miss.Renders = "Tifa (cache off)", ScenarioWriteEach, map[string]int{"vm": 5000}
		r.Runs = append(r.Runs, tifa, next, hit, miss)
	}
	return r
}

func TestMarkdownScenarios(t *testing.T) {
	md := roundTrip(t, withWrites(fixture())).Markdown()
	for _, want := range []string{
		"### Out of the box, reads with writes",
		"| Tifa | 120.0",
		"90.00%",
		"| Tifa | 500 | 3.50 | 9.5 |",
		"reads/s are reads over the time of reads and writes together",
		"Miss overhead of the render memo",
		"P50 31.00 ms with the cache, 30.00 ms without, +3.3%",
		"30.0 requests/s with the cache, 31.0 without, -3.2%",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	// The tables of reads hold only the runs of reads.
	read := md[strings.Index(md, "### Out of the box\n"):strings.Index(md, "### Out of the box, reads with writes")]
	if strings.Contains(read, "120.0") || strings.Contains(read, "| Tifa | 30.0") {
		t.Errorf("a run with writes is in the table of reads:\n%s", read)
	}
	eng := md[strings.Index(md, "### Engine"):strings.Index(md, "### Memory")]
	if strings.Contains(eng, "31.0") {
		t.Errorf("a run with writes is in the engine table:\n%s", eng)
	}
	if plain := fixture().Markdown(); strings.Contains(plain, "reads with writes") || strings.Contains(plain, "Miss overhead") {
		t.Errorf("a record without those scenarios prints their tables:\n%s", plain)
	}
}

func TestDiffComparesEveryScenario(t *testing.T) {
	old, bad := withWrites(fixture()), withWrites(fixture())
	if _, flagged := DiffRecords(old, bad); flagged != 0 {
		t.Fatalf("equal records flagged %d", flagged)
	}
	for i := range bad.Runs {
		if bad.Runs[i].Scenario == ScenarioReadWrite && bad.Runs[i].Spec == "tifa" {
			bad.Runs[i].Metrics.Requests *= 0.9
		}
	}
	lines, flagged := DiffRecords(old, bad)
	if flagged != 1 || !strings.Contains(strings.Join(lines, "\n"), ScenarioReadWrite) {
		t.Errorf("flagged %d, want the read-write requests/s:\n%s", flagged, strings.Join(lines, "\n"))
	}
}

// withWire adds the bytes on the wire (Tifa, Next.js and React Router gzip, Start sends plain
// HTML) and the two runs of Next.js that tell what gzip costs it.
func withWire(r *Record) *Record {
	for i := range r.Runs {
		run := &r.Runs[i]
		enc, size := "gzip", int64(10_000)
		if run.Framework == "start" {
			enc, size = "", 50_000
		}
		for _, p := range Pages {
			run.Wire = append(run.Wire, PageWire{Page: p.Name, Bytes: size, Encoding: enc})
		}
	}
	r.Settings.AcceptEncoding = "gzip"
	for round := 1; round <= 2; round++ {
		id := mkRun("nextjs-identity", "nextjs", round, 100, 10, ConfigCompression)
		id.Label, id.Reference = "Next.js, identity", true
		gz := mkRun("nextjs-gzip", "nextjs", round, 80, 12, ConfigCompression)
		gz.Label, gz.Reference = "Next.js, gzip", true
		for _, p := range Pages {
			id.Wire = append(id.Wire, PageWire{Page: p.Name, Bytes: 50_000})
			gz.Wire = append(gz.Wire, PageWire{Page: p.Name, Bytes: 10_000, Encoding: "gzip"})
		}
		r.Runs = append(r.Runs, id, gz)
	}
	return r
}

func TestMarkdownCompressionNote(t *testing.T) {
	r := withWire(fixture())
	r.Runs = append(r.Runs, mkRun("start", "start", 1, 300, 3, ConfigDefault, ConfigEngine))
	r.Runs[len(r.Runs)-1].Label = "TanStack Start"
	for _, p := range Pages {
		r.Runs[len(r.Runs)-1].Wire = append(r.Runs[len(r.Runs)-1].Wire, PageWire{Page: p.Name, Bytes: 50_000})
	}
	md := roundTrip(t, r).Markdown()
	for _, want := range []string{
		"Accept-Encoding: gzip",
		"TanStack Start sends 48.8 KB a page uncompressed, the others about 9.8 KB",
		"Next.js answers 20% fewer requests a second with gzip than without",
		"Start would likely pay a similar cost if it compressed",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	// The runs of the compression pair are in no table.
	if strings.Contains(md, "Next.js, gzip |") || strings.Contains(md, "Next.js, identity |") {
		t.Errorf("a compression run is in a table:\n%s", md)
	}
	if plain := fixture().Markdown(); strings.Contains(plain, "a page uncompressed") {
		t.Errorf("a record without wire sizes prints the note:\n%s", plain)
	}
}
