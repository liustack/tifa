package compare

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// JSON is the report as JSON.
func (r *Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", " ") }

func (r *Report) servers() []string {
	var names []string
	for _, res := range r.Results {
		if !slices.Contains(names, res.Server) {
			names = append(names, res.Server)
		}
	}
	return names
}

// pick returns the results of one server for a page ("" for the six in turn), by round.
func (r *Report) pick(server, page string) []Result {
	var list []Result
	for _, res := range r.Results {
		if res.Server == server && res.Page == page {
			list = append(list, res)
		}
	}
	slices.SortFunc(list, func(a, b Result) int { return a.Round - b.Round })
	return list
}

func join[T any](list []T, f func(T) string) string {
	var parts []string
	for _, x := range list {
		parts = append(parts, f(x))
	}
	return strings.Join(parts, ", ")
}

// span writes the smallest and the largest of the values, or the one value.
func span(values []float64, format string) string {
	if len(values) == 0 {
		return "-"
	}
	lo, hi := slices.Min(values), slices.Max(values)
	if fmt.Sprintf(format, lo) == fmt.Sprintf(format, hi) {
		return fmt.Sprintf(format, lo)
	}
	return fmt.Sprintf(format+" to "+format, lo, hi)
}

func field(list []Result, f func(Result) float64) []float64 {
	var out []float64
	for _, x := range list {
		out = append(out, f(x))
	}
	return out
}

// Markdown writes the tables: the six pages in turn, each page alone, and the split of tifa's
// request into the render and the rest.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Measured %s. ", r.Date)
	fmt.Fprintf(&b, "Server on cpus %q, load on cpus %q, %v rounds of %v after %v of warm-up.\n\n", r.Options["serverCpus"], r.Options["loadCpus"], r.Options["rounds"], r.Options["duration"], r.Options["warmup"])
	tifa := r.pick("tifa", "")

	b.WriteString("| | requests/s, by round | times tifa, by round | P50 ms | P99 ms | peak RSS MiB |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, name := range r.servers() {
		list := r.pick(name, "")
		if len(list) == 0 {
			continue
		}
		times := "-"
		if len(tifa) == len(list) {
			var t []string
			for i := range list {
				t = append(t, fmt.Sprintf("%.2f", list[i].Requests/tifa[i].Requests))
			}
			times = strings.Join(t, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", name,
			join(list, func(x Result) string { return fmt.Sprintf("%.1f", x.Requests) }), times,
			span(field(list, func(x Result) float64 { return x.P50 }), "%.2f"),
			span(field(list, func(x Result) float64 { return x.P99 }), "%.1f"),
			span(field(list, func(x Result) float64 { return x.PeakRSS }), "%.0f"))
	}

	if len(r.pick(r.servers()[0], Pages[0].Name)) > 0 {
		b.WriteString("\nOne page at a time, requests/s (P50 ms), the mean of the rounds:\n\n| page |")
		for _, name := range r.servers() {
			fmt.Fprintf(&b, " %s |", name)
		}
		b.WriteString("\n| --- |" + strings.Repeat(" --- |", len(r.servers())) + "\n")
		for _, p := range Pages {
			fmt.Fprintf(&b, "| %s |", p.Name)
			for _, name := range r.servers() {
				list := r.pick(name, p.Name)
				fmt.Fprintf(&b, " %.1f (%.2f) |", mean(field(list, func(x Result) float64 { return x.Requests })), mean(field(list, func(x Result) float64 { return x.P50 })))
			}
			b.WriteString("\n")
		}
	}

	if len(r.Render) == len(Pages) && len(r.pick("tifa", Pages[0].Name)) > 0 {
		b.WriteString("\ntifa, one request at a time: the whole request and the render alone (P50 ms), and what the rest of the request costs, which is routing, `Load`, JSON, writing the HTML and net/http:\n\n")
		b.WriteString("| page | request | render | the rest | share of the request |\n| --- | --- | --- | --- | --- |\n")
		var req, ren float64
		for i, p := range Pages {
			http := mean(field(r.pick("tifa", p.Name), func(x Result) float64 { return x.P50 }))
			rest := http - r.Render[i].P50
			fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.0f%% |\n", p.Name, http, r.Render[i].P50, rest, rest/http*100)
			req += http
			ren += r.Render[i].P50
		}
		fmt.Fprintf(&b, "| mean of the six | %.2f | %.2f | %.2f | %.0f%% |\n", req/6, ren/6, (req-ren)/6, (req-ren)/req*100)
	}
	return b.String()
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
