package compare

import (
	"fmt"
	"slices"
	"strings"
)

// cell writes the median of the values with their range when the rounds differ.
func cell(values []float64, d *metricDef) string {
	if len(values) == 0 {
		return "-"
	}
	med := Median(values)
	lo, hi := Range(values)
	s := d.Format(med)
	if d.Format(lo) != d.Format(hi) {
		s += fmt.Sprintf(" (%s to %s)", d.Format(lo), d.Format(hi))
	}
	return s
}

// Markdown writes the tables: what each framework does as shipped, with the engine alone (all
// caches off, and the Node servers without their JIT as reference), and the memory.
func (r *Record) Markdown() string {
	var b strings.Builder
	v := r.Versions
	fmt.Fprintf(&b, "Measured %s. Go %s, quickjs %s, Node %s", r.Date, strings.TrimPrefix(v["go"], "go"), v["quickjs"], v["node"])
	var extra []string
	for _, k := range sortedKeys(v) {
		if !slices.Contains([]string{"go", "quickjs", "node"}, k) {
			extra = append(extra, k+" "+v[k])
		}
	}
	if len(extra) > 0 {
		b.WriteString(", " + strings.Join(extra, ", "))
	}
	fmt.Fprintf(&b, ". %s, %s.\n", r.Machine.CPU, r.Machine.Kernel)
	fmt.Fprintf(&b, "Placement: %s", r.Placement.Note)
	if r.Placement.Server != "" {
		fmt.Fprintf(&b, " (server cpus %s, load cpus %s)", r.Placement.Server, r.Placement.Load)
	}
	fmt.Fprintf(&b, ". %d rounds, fresh servers each, %s of steady load after %d cold requests, no warm-up. Each cell is the median of the rounds, with their range in brackets when they differ.\n", r.Settings.Rounds, r.Settings.Steady, r.Settings.Cold)

	b.WriteString("\n### Out of the box\n\nEach framework as shipped: Tifa with its render cache at the default size.\n\n")
	r.speedTable(&b, "default")
	b.WriteString("\n### Engine\n\nEvery cache off: Tifa with `cache.size: 0`. Rows marked reference are Node with `--jitless`, to show what the JIT is worth.\n\n")
	r.speedTable(&b, "engine")
	b.WriteString("\n### Memory\n\n")
	r.memoryTable(&b)
	if len(r.AA) > 0 {
		b.WriteString("\nA/A, Tifa against Tifa, the spread between two identical servers (largest minus smallest over their median):")
		var parts []string
		for _, d := range metricDefs {
			if s, ok := r.AA[d.Key]; ok {
				parts = append(parts, fmt.Sprintf("%s %.1f%%", d.Name, s*100))
			}
		}
		b.WriteString(" " + strings.Join(parts, ", ") + ".\n")
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (r *Record) specsOf(configs ...string) (order []string, by map[string][]Run) {
	order, all := group(r.Runs)
	by = map[string][]Run{}
	var keep []string
	for _, id := range order {
		for _, c := range configs {
			if slices.Contains(all[id][0].Configs, c) {
				keep = append(keep, id)
				by[id] = all[id]
				break
			}
		}
	}
	return keep, by
}

func label(runs []Run) string {
	if runs[0].Reference {
		return runs[0].Label + " (reference)"
	}
	return runs[0].Label
}

var speedKeys = []string{"requests", "p50", "p99", "ratio", "max", "rssPeak", "firstAnswer", "first100", "first1000", "settle", "hitRate"}

func (r *Record) speedTable(b *strings.Builder, config string) {
	order, by := r.specsOf(config)
	b.WriteString("| | requests/s | against Tifa")
	for _, k := range speedKeys[1:] {
		b.WriteString(" | " + defByKey(k).Name)
	}
	b.WriteString(" |\n| --- |" + strings.Repeat(" --- |", len(speedKeys)+1) + "\n")
	var tifa float64
	for _, id := range order {
		if by[id][0].Framework == "tifa" {
			tifa = Median(values(by[id], defByKey("requests")))
			break
		}
	}
	for _, id := range order {
		runs := by[id]
		fmt.Fprintf(b, "| %s | %s | ", label(runs), cell(values(runs, defByKey("requests")), defByKey("requests")))
		if tifa > 0 {
			fmt.Fprintf(b, "%.2f", Median(values(runs, defByKey("requests")))/tifa)
		} else {
			b.WriteString("-")
		}
		for _, k := range speedKeys[1:] {
			d := defByKey(k)
			b.WriteString(" | " + cell(values(runs, d), d))
		}
		b.WriteString(" |\n")
	}
}

func (r *Record) memoryTable(b *strings.Builder) {
	order, by := r.specsOf("default", "engine")
	b.WriteString("| | RSS after start MiB | peak RSS MiB |\n| --- | --- | --- |\n")
	for _, id := range order {
		runs := by[id]
		fmt.Fprintf(b, "| %s | %s | %s |\n", label(runs), cell(values(runs, defByKey("rssStart")), defByKey("rssStart")), cell(values(runs, defByKey("rssPeak")), defByKey("rssPeak")))
	}
}
