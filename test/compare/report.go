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
	r.speedTable(&b, "default", ScenarioRead)
	if r.hasScenario(ScenarioReadWrite) {
		b.WriteString("\n### Out of the box, reads with writes\n\nThe same reads, and before the first order page after each 10 reads one write that gives that order a new address, so that the render cache of Tifa misses on it. Only the reads are timed. Lists and stats do not show the address, so writes come before order pages only. The hit rate is Tifa's.\n\n")
		r.speedTable(&b, "default", ScenarioReadWrite)
	}
	b.WriteString("\n### Engine\n\nEvery cache off: Tifa with `cache.size: 0`. Rows marked reference are Node with `--jitless`, to show what the JIT is worth.\n\n")
	r.speedTable(&b, "engine", ScenarioRead)
	b.WriteString("\n### Memory\n\n")
	r.memoryTable(&b)
	if r.hasPages() {
		b.WriteString("\n### Per page\n")
		r.pageTables(&b)
	}
	r.renderTable(&b)
	r.missOverhead(&b)
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

func (r *Record) specsOf(scenario string, configs ...string) (order []string, by map[string][]Run) {
	order, all := group(r.Runs, scenario)
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

func (r *Record) speedTable(b *strings.Builder, config, scenario string) {
	order, by := r.specsOf(scenario, config)
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
	order, by := r.specsOf(ScenarioRead, "default", "engine")
	b.WriteString("| | RSS after start MiB | peak RSS MiB |\n| --- | --- | --- |\n")
	for _, id := range order {
		runs := by[id]
		fmt.Fprintf(b, "| %s | %s | %s |\n", label(runs), cell(values(runs, defByKey("rssStart")), defByKey("rssStart")), cell(values(runs, defByKey("rssPeak")), defByKey("rssPeak")))
	}
}

// pageValues returns the per-page numbers of the runs of a spec for one page, by round.
func pageValues(runs []Run, page string) (p50, req []float64) {
	for _, r := range runs {
		for _, p := range r.Pages {
			if p.Page == page {
				p50, req = append(p50, p.P50), append(req, p.Requests)
			}
		}
	}
	return p50, req
}

// pageTables writes, for each configuration, the steady P50 and requests a second of every page
// for every server, and for the Node servers how many times Tifa's rate that is.
func (r *Record) pageTables(b *strings.Builder) {
	for _, config := range Configs {
		order, by := r.specsOf(ScenarioRead, config)
		if len(order) == 0 || len(by[order[0]][0].Pages) == 0 {
			continue
		}
		ref := ""
		for _, id := range order {
			if by[id][0].Framework == "tifa" {
				ref = id
				break
			}
		}
		fmt.Fprintf(b, "\n%s: requests/s with one request in flight (P50 ms), and for the others how many times Tifa's rate that is:\n\n| page |", config)
		for _, id := range order {
			b.WriteString(" " + label(by[id]) + " |")
		}
		b.WriteString("\n| --- |" + strings.Repeat(" --- |", len(order)) + "\n")
		for _, p := range Pages {
			fmt.Fprintf(b, "| %s |", p.Name)
			_, refReq := pageValues(by[ref], p.Name)
			for _, id := range order {
				p50, req := pageValues(by[id], p.Name)
				if len(req) == 0 {
					b.WriteString(" - |")
					continue
				}
				fmt.Fprintf(b, " %.1f (%.2f ms)", Median(req), Median(p50))
				if id != ref && ref != "" && len(refReq) > 0 && Median(refReq) > 0 {
					fmt.Fprintf(b, ", %.2fx", Median(req)/Median(refReq))
				}
				b.WriteString(" |")
			}
			b.WriteString("\n")
		}
	}
}

// renderTable splits a request of Tifa with the cache off into the render and the rest, which is
// routing, Load, JSON, writing the HTML and net/http.
func (r *Record) renderTable(b *strings.Builder) {
	if len(r.Render) != len(Pages) {
		return
	}
	var runs []Run
	for _, run := range r.Runs {
		if run.Spec == "tifa-nocache" && scenarioOf(run) == ScenarioRead {
			runs = append(runs, run)
		}
	}
	if len(runs) == 0 || len(runs[0].Pages) == 0 {
		return
	}
	b.WriteString("\n### Render and the rest\n\nTifa with the cache off, one request at a time: the whole request and the render alone in a warm VM without HTTP (P50 ms), and what the rest costs.\n\n")
	b.WriteString("| page | request | render | the rest | share of the request |\n| --- | --- | --- | --- | --- |\n")
	var req, ren float64
	for _, p := range Pages {
		p50, _ := pageValues(runs, p.Name)
		whole := Median(p50)
		var render float64
		for _, x := range r.Render {
			if x.Page == p.Name {
				render = x.P50
			}
		}
		rest := whole - render
		fmt.Fprintf(b, "| %s | %.2f | %.2f | %.2f | %.0f%% |\n", p.Name, whole, render, rest, rest/whole*100)
		req += whole
		ren += render
	}
	n := float64(len(Pages))
	fmt.Fprintf(b, "| mean of the six | %.2f | %.2f | %.2f | %.0f%% |\n", req/n, ren/n, (req-ren)/n, (req-ren)/req*100)
}

func (r *Record) hasPages() bool {
	for _, run := range r.Runs {
		if len(run.Pages) > 0 && scenarioOf(run) == ScenarioRead {
			return true
		}
	}
	return false
}

func (r *Record) hasScenario(name string) bool {
	return slices.ContainsFunc(r.Runs, func(run Run) bool { return scenarioOf(run) == name })
}

// missOverhead writes what the render cache costs a request that misses, from the runs in which
// every read of an order page follows a write: Tifa with the cache on, which never hits, against
// Tifa with the cache off.
func (r *Record) missOverhead(b *strings.Builder) {
	var on, off []Run
	for _, run := range r.Runs {
		if scenarioOf(run) != ScenarioWriteEach {
			continue
		}
		switch run.Spec {
		case "tifa":
			on = append(on, run)
		case "tifa-nocache":
			off = append(off, run)
		}
	}
	if len(on) == 0 || len(off) == 0 {
		return
	}
	p50, req := defByKey("p50"), defByKey("requests")
	onP50, offP50 := Median(values(on, p50)), Median(values(off, p50))
	onReq, offReq := Median(values(on, req)), Median(values(off, req))
	if offP50 == 0 || offReq == 0 {
		return
	}
	fmt.Fprintf(b, "\nMiss overhead of the render cache, with a write before every read of an order page so that it never hits: P50 %.2f ms with the cache, %.2f ms without, %+.1f%%. %.1f requests/s with the cache, %.1f without, %+.1f%%.\n",
		onP50, offP50, (onP50-offP50)/offP50*100, onReq, offReq, (onReq-offReq)/offReq*100)
}
