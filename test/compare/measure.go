package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Options say how to measure. The method is the one of docs/studies/nextjs-and-tanstack-start.md:
// each server starts fresh for each round, warms up, then answers the six pages in turn to one
// client that has one request in flight, asking for no compression.
type Options struct {
	ServerCPUs string        // for taskset, such as "0". Empty: no pinning, which is all there is off Linux
	LoadCPUs   string        // for oha, such as "4"
	Rounds     int           // fresh servers per round
	Warmup     time.Duration // before the measured run
	Duration   time.Duration // of the measured run
	Servers    []string      // by name, empty for all four

	// PerPage also runs each page alone.
	PerPage      bool
	PageWarmup   time.Duration
	PageDuration time.Duration

	// Render also runs the render benchmark of test/ssr, which renders the same six pages in one
	// VM without HTTP, to split a request into the render and the rest. It needs PerPage.
	Render bool

	Log io.Writer
}

// Result is one run of oha against one server. Page is empty for the six pages in turn.
type Result struct {
	Server    string         `json:"server"`
	Round     int            `json:"round"`
	Page      string         `json:"page,omitempty"`
	Requests  float64        `json:"requestsPerSecond"`
	P50       float64        `json:"p50Ms"`
	P99       float64        `json:"p99Ms"`
	PeakRSS   float64        `json:"peakRssMiB"`
	BytesEach float64        `json:"bytesPerAnswer"`
	Status    map[string]int `json:"status"`
	Errors    map[string]int `json:"errors,omitempty"`
}

// RenderResult is the render of one page in a warm VM of tifa, without HTTP, in milliseconds.
type RenderResult struct {
	Page string  `json:"page"`
	P50  float64 `json:"p50Ms"`
	P99  float64 `json:"p99Ms"`
}

// Report is what a measurement leaves.
type Report struct {
	Date    string            `json:"date"`
	Machine map[string]string `json:"machine"`
	Options map[string]any    `json:"options"`
	Results []Result          `json:"results"`
	Render  []RenderResult    `json:"render,omitempty"`
}

// Measure runs the measurement. Prepare must have run.
func (e Env) Measure(ctx context.Context, o Options) (*Report, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	report := &Report{
		Date:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Machine: machine(),
		Options: map[string]any{
			"serverCpus": o.ServerCPUs, "loadCpus": o.LoadCPUs, "rounds": o.Rounds,
			"warmup": o.Warmup.String(), "duration": o.Duration.String(),
			"perPage": o.PerPage, "pageWarmup": o.PageWarmup.String(), "pageDuration": o.PageDuration.String(),
		},
	}
	var targets []Target
	for _, t := range e.Targets() {
		if len(o.Servers) == 0 || slices.Contains(o.Servers, t.Name) {
			targets = append(targets, t)
		}
	}
	urls := filepath.Join(os.TempDir(), fmt.Sprintf("tifa-compare-%d-urls.txt", os.Getpid()))
	defer os.Remove(urls)

	for round := 1; round <= o.Rounds; round++ {
		for _, t := range targets {
			if err := waitIdle(ctx, o.Log, o.ServerCPUs+","+o.LoadCPUs); err != nil {
				return nil, err
			}
			server, err := t.Start(ctx, o.ServerCPUs)
			if err != nil {
				return nil, err
			}
			err = func() error {
				defer server.Stop()
				var lines []string
				for _, p := range Pages {
					lines = append(lines, server.Base+p.URL)
				}
				if err := os.WriteFile(urls, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
					return err
				}
				if err := warmRun(ctx, o, o.Warmup, "--urls-from-file", urls); err != nil {
					return err
				}
				res, err := measureRun(ctx, o, o.Duration, "--urls-from-file", urls)
				if err != nil {
					return err
				}
				res.Server, res.Round, res.PeakRSS = t.Name, round, float64(server.PeakRSS())/(1<<20)
				report.Results = append(report.Results, *res)
				fmt.Fprintf(o.Log, "round %d %-15s %7.1f requests/s  P50 %6.2f ms  P99 %6.2f ms  peak RSS %5.0f MiB\n", round, t.Name, res.Requests, res.P50, res.P99, res.PeakRSS)
				if !o.PerPage {
					return nil
				}
				for _, p := range Pages {
					if err := warmRun(ctx, o, o.PageWarmup, server.Base+p.URL); err != nil {
						return err
					}
					res, err := measureRun(ctx, o, o.PageDuration, server.Base+p.URL)
					if err != nil {
						return err
					}
					res.Server, res.Round, res.Page, res.PeakRSS = t.Name, round, p.Name, float64(server.PeakRSS())/(1<<20)
					report.Results = append(report.Results, *res)
					fmt.Fprintf(o.Log, "round %d %-15s %-14s %7.1f requests/s  P50 %6.2f ms  P99 %6.2f ms\n", round, t.Name, p.Name, res.Requests, res.P50, res.P99)
				}
				return nil
			}()
			if err != nil {
				return nil, err
			}
		}
	}
	if o.Render {
		render, err := e.renderBench(ctx, o)
		if err != nil {
			return nil, err
		}
		report.Render = render
	}
	return report, nil
}

// pin puts taskset in front of a command when cpus are given.
func pin(cpus string, name string, args ...string) (string, []string) {
	if cpus == "" || runtime.GOOS != "linux" {
		return name, args
	}
	return "taskset", append([]string{"-c", cpus, name}, args...)
}

func ohaArgs(d time.Duration, format string, rest ...string) []string {
	return append([]string{"--no-tui", "-w", "-z", d.String(), "-c", "1", "--worker-threads", "1", "--output-format", format, "-H", "Accept-Encoding: identity"}, rest...)
}

// warmRun runs the load tool and drops its answer: a warm-up.
func warmRun(ctx context.Context, o Options, d time.Duration, target ...string) error {
	name, args := pin(o.LoadCPUs, "oha", ohaArgs(d, "quiet", target...)...)
	if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("oha: %w\n%s", err, out)
	}
	return nil
}

// measureRun runs the load tool and reads its JSON.
func measureRun(ctx context.Context, o Options, d time.Duration, target ...string) (*Result, error) {
	name, args := pin(o.LoadCPUs, "oha", ohaArgs(d, "json", target...)...)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("oha: %w\n%s", err, stderr.String())
	}
	var j struct {
		Summary struct {
			RequestsPerSec float64  `json:"requestsPerSec"`
			SizePerRequest *float64 `json:"sizePerRequest"`
		} `json:"summary"`
		Latency struct {
			P50 float64 `json:"p50"`
			P99 float64 `json:"p99"`
		} `json:"latencyPercentiles"`
		Status map[string]int `json:"statusCodeDistribution"`
		Errors map[string]int `json:"errorDistribution"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("oha printed no JSON: %w\n%s", err, out)
	}
	r := &Result{Requests: j.Summary.RequestsPerSec, P50: j.Latency.P50 * 1e3, P99: j.Latency.P99 * 1e3, Status: j.Status, Errors: j.Errors}
	if j.Summary.SizePerRequest != nil {
		r.BytesEach = *j.Summary.SizePerRequest
	}
	for code, n := range j.Status {
		if code != "200" && n > 0 {
			return nil, fmt.Errorf("%d answers with status %s", n, code)
		}
	}
	if len(j.Errors) > 0 {
		return nil, fmt.Errorf("oha errors: %v", j.Errors)
	}
	return r, nil
}

// waitIdle waits, on Linux, until the given cpus have used under 0.1 of a CPU for 3 seconds, so
// that a server does not start next to the tail of the last one. Other platforms wait 3 seconds.
func waitIdle(ctx context.Context, log io.Writer, cpus string) error {
	if runtime.GOOS != "linux" || strings.Trim(cpus, ",") == "" {
		return sleep(ctx, 3*time.Second)
	}
	want := map[string]bool{}
	for _, c := range strings.Split(cpus, ",") {
		if c != "" {
			want["cpu"+c] = true
		}
	}
	busy := func() int64 {
		data, _ := os.ReadFile("/proc/stat")
		var sum int64
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || !want[f[0]] {
				continue
			}
			// user nice system idle iowait irq softirq steal: all but idle and iowait
			for _, i := range []int{1, 2, 3, 6, 7, 8} {
				n, _ := strconv.ParseInt(f[i], 10, 64)
				sum += n
			}
		}
		return sum
	}
	start := time.Now()
	for {
		a := busy()
		if err := sleep(ctx, 3*time.Second); err != nil {
			return err
		}
		if busy()-a < 30 {
			if w := time.Since(start) - 3*time.Second; w > time.Second {
				fmt.Fprintf(log, "waited %s for cpus %s to be idle\n", w.Round(time.Second), cpus)
			}
			return nil
		}
		if time.Since(start) > 30*time.Minute {
			return fmt.Errorf("cpus %s were not idle after 30 minutes", cpus)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// renderBench runs BenchmarkShop of test/ssr, which renders the six pages in a warm VM with their
// data in hand, and returns the P50 and P99 of a render of each. It is the same engine, bundle and
// data as the tifa program uses, without the request around it.
func (e Env) renderBench(ctx context.Context, o Options) ([]RenderResult, error) {
	name, args := pin(o.ServerCPUs, "go", "test", "-run", "^$", "-bench", "Shop/^render", "-benchtime", "2000x", "-count", "1", "./test/ssr")
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.Root
	cmd.Env = append(os.Environ(), "TIFA_SSR_SHOP=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("render benchmark: %w\n%s", err, out)
	}
	var results []RenderResult
	suffix := regexp.MustCompile(`^-\d+$`)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[0], "BenchmarkShop/render") {
			continue
		}
		var p50, p99 float64
		for i := 2; i+1 < len(f); i += 2 {
			switch f[i+1] {
			case "p50-ms":
				p50, _ = strconv.ParseFloat(f[i], 64)
			case "p99-ms":
				p99, _ = strconv.ParseFloat(f[i], 64)
			}
		}
		for _, p := range Pages {
			want := "BenchmarkShop/render" + strings.ReplaceAll(p.URL, "/", "-")
			if f[0] == want || strings.HasPrefix(f[0], want) && suffix.MatchString(f[0][len(want):]) {
				results = append(results, RenderResult{Page: p.Name, P50: p50, P99: p99})
			}
		}
	}
	if len(results) != len(Pages) {
		return nil, fmt.Errorf("render benchmark printed %d of the %d pages:\n%s", len(results), len(Pages), out)
	}
	return results, nil
}

func machine() map[string]string {
	m := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()}
	for name, args := range map[string][]string{"node": {"--version"}, "oha": {"--version"}, "pnpm": {"--version"}} {
		if out, err := exec.Command(name, args...).Output(); err == nil {
			m[name] = strings.TrimSpace(string(out))
		}
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				m["cpu"] = strings.TrimSpace(v)
				break
			}
		}
	}
	return m
}
