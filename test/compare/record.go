package compare

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"slices"
)

// Record is what `compare measure` leaves: the runs of every spec in every round, and what is
// needed to judge them, which are the versions, the machine, the placement and the settings.
// `compare report` and `compare diff` read it back.
type Record struct {
	Date      string            `json:"date"`
	Versions  map[string]string `json:"versions"`
	Machine   Machine           `json:"machine"`
	Placement Placement         `json:"placement"`
	Settings  Settings          `json:"settings"`
	Runs      []Run             `json:"runs"`
	// AA is, for each metric key, how far apart two identical Tifa servers were in this record:
	// the largest minus the smallest value over the A/A runs, divided by their median. Absent
	// when the record has no A/A runs.
	AA map[string]float64 `json:"aa,omitempty"`
	// Render is the render of each page in a warm VM without HTTP, when measured with -render.
	Render []RenderResult `json:"render,omitempty"`
}

// Machine is where it ran.
type Machine struct {
	CPU    string `json:"cpu"`
	Kernel string `json:"kernel"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

// Settings are the flags of the measurement that shape the numbers.
type Settings struct {
	Rounds    int      `json:"rounds"`
	Steady    string   `json:"steady"`
	Cold      int      `json:"coldRequests"`
	Configs   []string `json:"configurations"`
	Scenarios []string `json:"scenarios"`
	// AcceptEncoding is what the client asked for in every request, except for the specs that force
	// their own (the compression pair): gzip or identity.
	AcceptEncoding string `json:"acceptEncoding"`
}

// Run is one server started fresh in one round.
type Run struct {
	Spec      string   `json:"spec"`      // the id of the Spec, such as "tifa-nocache"
	Framework string   `json:"framework"` // "tifa", "nextjs", "start", ...
	Label     string   `json:"label"`     // for tables
	Configs   []string `json:"configurations"`
	Reference bool     `json:"reference,omitempty"` // for explanation, such as --jitless
	AA        bool     `json:"aa,omitempty"`        // one of the two servers of the A/A check
	Round     int      `json:"round"`
	Scenario  string   `json:"scenario"`
	Metrics   Metrics  `json:"metrics"`
	// Renders counts the answers by their X-Tifa-Render header ("vm", "cache", "shared") over the
	// whole run. Only Tifa sets it.
	Renders map[string]int `json:"renders,omitempty"`
	// Pages are the steady phase by page.
	Pages []PageStat `json:"pages,omitempty"`
	// Wire is what each page weighed on the wire as the client got it, and how it was encoded.
	Wire []PageWire `json:"wire,omitempty"`
}

// PageWire is the body of a page as it came over the connection: its length and Content-Encoding
// (empty when the server did not compress it).
type PageWire struct {
	Page     string `json:"page"`
	Bytes    int64  `json:"bytes"`
	Encoding string `json:"encoding,omitempty"`
}

// PageStat is the steady phase of one page. Requests is the rate that one request in flight gives
// for the page alone: 1000 over its mean latency in milliseconds.
type PageStat struct {
	Page     string  `json:"page"`
	P50      float64 `json:"p50Ms"`
	Requests float64 `json:"requestsPerSecond"`
}

// pageStats splits latencies (ms) by the page of each request, in the order of Pages.
func pageStats(latencies []float64, pages []int) []PageStat {
	by := map[int][]float64{}
	for i, l := range latencies {
		by[pages[i]] = append(by[pages[i]], l)
	}
	var out []PageStat
	for i, p := range Pages {
		if len(by[i]) == 0 {
			continue
		}
		sorted := slices.Sorted(slices.Values(by[i]))
		stat := PageStat{Page: p.Name, P50: Percentile(sorted, 50)}
		if m := mean(sorted); m > 0 {
			stat.Requests = 1000 / m
		}
		out = append(out, stat)
	}
	return out
}

// SettleNever is what Metrics.Settle holds when latency had not settled within the cold series.
const SettleNever = 1001

// Metrics are the numbers of one run. Times are in milliseconds, sizes in MiB. The latencies and
// the rate are of the steady phase, the first answer, the first 100 and 1000 requests and the
// settling point are of the series from process start.
type Metrics struct {
	Requests    float64 `json:"requestsPerSecond"`
	P50         float64 `json:"p50Ms"`
	P99         float64 `json:"p99Ms"`
	Max         float64 `json:"maxMs"`
	FirstAnswer float64 `json:"firstAnswerMs"`  // from the start of the process to the end of the first answer
	First100    float64 `json:"first100MeanMs"` // mean latency of the first 100 requests
	First1000   float64 `json:"first1000MeanMs"`
	Settle      float64 `json:"settlesAtRequest"` // SettleNever when it had not
	RSSStart    float64 `json:"rssAfterStartMiB"` // after the first answer
	RSSPeak     float64 `json:"peakRssMiB"`
	HitRate     float64 `json:"hitRate"` // Tifa only: answers with X-Tifa-Render cache or shared
}

// metricDef describes one column of the tables and one line of the diff.
type metricDef struct {
	Key, Name string
	Higher    bool // a higher value is better
	Get       func(*Run) (float64, bool)
	Format    func(float64) string
}

func always(f func(*Metrics) float64) func(*Run) (float64, bool) {
	return func(r *Run) (float64, bool) { return f(&r.Metrics), true }
}

func fixed(format string) func(float64) string {
	return func(v float64) string { return fmt.Sprintf(format, v) }
}

// metricDefs are all the metrics, in the order of the table columns.
var metricDefs = []metricDef{
	{"requests", "requests/s", true, always(func(m *Metrics) float64 { return m.Requests }), fixed("%.1f")},
	{"p50", "P50 ms", false, always(func(m *Metrics) float64 { return m.P50 }), fixed("%.2f")},
	{"p99", "P99 ms", false, always(func(m *Metrics) float64 { return m.P99 }), fixed("%.1f")},
	{"ratio", "P99/P50", false, always(func(m *Metrics) float64 {
		if m.P50 == 0 {
			return 0
		}
		return m.P99 / m.P50
	}), fixed("%.1f")},
	{"max", "max ms", false, always(func(m *Metrics) float64 { return m.Max }), fixed("%.1f")},
	{"rssPeak", "peak RSS MiB", false, always(func(m *Metrics) float64 { return m.RSSPeak }), fixed("%.0f")},
	{"rssStart", "RSS after start MiB", false, always(func(m *Metrics) float64 { return m.RSSStart }), fixed("%.0f")},
	{"firstAnswer", "first answer ms", false, always(func(m *Metrics) float64 { return m.FirstAnswer }), fixed("%.0f")},
	{"first100", "first 100 mean ms", false, always(func(m *Metrics) float64 { return m.First100 }), fixed("%.2f")},
	{"first1000", "first 1000 mean ms", false, always(func(m *Metrics) float64 { return m.First1000 }), fixed("%.2f")},
	{"settle", "settles at request", false, always(func(m *Metrics) float64 { return m.Settle }), func(v float64) string {
		if v >= SettleNever {
			return ">1000"
		}
		return fmt.Sprintf("%.0f", v)
	}},
	{"hitRate", "hit rate", true, func(r *Run) (float64, bool) { return r.Metrics.HitRate, r.Renders != nil }, func(v float64) string {
		return fmt.Sprintf("%.2f%%", v*100)
	}},
}

func defByKey(key string) *metricDef {
	for i := range metricDefs {
		if metricDefs[i].Key == key {
			return &metricDefs[i]
		}
	}
	return nil
}

// values returns one metric of the runs that have it.
func values(runs []Run, d *metricDef) []float64 {
	var out []float64
	for i := range runs {
		if v, ok := d.Get(&runs[i]); ok {
			out = append(out, v)
		}
	}
	return out
}

// AASpread is how far apart the A/A runs were, for each metric: (largest - smallest) / median
// over all of them. It is empty for fewer than two runs.
func AASpread(runs []Run) map[string]float64 {
	var aa []Run
	for _, r := range runs {
		if r.AA {
			aa = append(aa, r)
		}
	}
	out := map[string]float64{}
	if len(aa) < 2 {
		return out
	}
	for i := range metricDefs {
		d := &metricDefs[i]
		v := values(aa, d)
		lo, hi := Range(v)
		if med := Median(v); len(v) > 1 && med != 0 {
			out[d.Key] = (hi - lo) / math.Abs(med)
		} else if len(v) > 1 {
			out[d.Key] = 0
		}
	}
	return out
}

// JSON is the record as indented JSON.
func (r *Record) JSON() ([]byte, error) { return json.Marshal(r, jsontext.WithIndent(" ")) }

// ReadRecord parses a record written by JSON.
func ReadRecord(data []byte) (*Record, error) {
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// scenarioOf is the scenario of a run, which older records leave out for reads.
func scenarioOf(r Run) string {
	if r.Scenario == "" {
		return ScenarioRead
	}
	return r.Scenario
}

// group splits the runs of a scenario, A/A runs left out, by spec, in the order the specs first appear.
func group(runs []Run, scenario string) (order []string, by map[string][]Run) {
	by = map[string][]Run{}
	for _, r := range runs {
		if r.AA || scenarioOf(r) != scenario {
			continue
		}
		if _, ok := by[r.Spec]; !ok {
			order = append(order, r.Spec)
		}
		by[r.Spec] = append(by[r.Spec], r)
	}
	return order, by
}

// DiffRecords compares the Tifa specs of two records and returns a line for each metric that got worse
// by more than max(2%, A/A spread), and how many that are. The spread is the larger one of the
// two records. Other frameworks are not compared, since a change to Tifa cannot move them.
func DiffRecords(old, new *Record) (lines []string, flagged int) {
	var scenarios []string
	for _, r := range new.Runs {
		if s := scenarioOf(r); !slices.Contains(scenarios, s) {
			scenarios = append(scenarios, s)
		}
	}
	for _, scenario := range scenarios {
		_, oldBy := group(old.Runs, scenario)
		order, newBy := group(new.Runs, scenario)
		for _, id := range order {
			o, ok := oldBy[id]
			n := newBy[id]
			if !ok || n[0].Framework != "tifa" {
				continue
			}
			name := id
			if scenario != ScenarioRead {
				name += " (" + scenario + ")"
			}
			for i := range metricDefs {
				d := &metricDefs[i]
				ov, nv := values(o, d), values(n, d)
				if len(ov) == 0 || len(nv) == 0 {
					continue
				}
				om, nm := Median(ov), Median(nv)
				if om == 0 {
					continue
				}
				limit := max(0.02, old.AA[d.Key], new.AA[d.Key])
				change := (nm - om) / om
				worse := change < -limit
				if !d.Higher {
					worse = change > limit
				}
				if worse {
					flagged++
					lines = append(lines, fmt.Sprintf("%s %s: %s -> %s (%+.1f%%, limit %.1f%%)", name, d.Name, d.Format(om), d.Format(nm), change*100, limit*100))
				}
			}
		}
	}
	return lines, flagged
}
