package compare

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

// Options say how to measure. The method: each round starts every server fresh and times it from
// the start of its process, with no warm-up. The first answer, the first 100 and the first 1000
// requests are the cold part. Then comes a steady phase. One client has one request in flight and
// asks for no compression.
type Options struct {
	ServerCPUs     string        // for taskset, such as "1". Empty: no pinning, which is all there is off Linux
	LoadCPUs       string        // for this process, which is the client, such as "2"
	Rounds         int           // fresh servers each round
	Steady         time.Duration // length of the steady phase
	Cold           int           // requests of the cold series, 1000 when 0
	Configs        []string      // configurations to measure, both when empty
	Servers        []string      // spec ids, all of the configurations when empty
	AA             bool          // also run Tifa against Tifa first in each round
	AcceptEncoding string        // gzip when empty, or identity: what the client asks for in every request
	Render         bool          // also run the render benchmark of test/ssr once after the rounds, apart from the cold runs
	Scenarios      []Scenario    // ReadScenario only when empty
	Procs          int           // VMs and CPUs of Tifa and requests in flight, one connection each. 1 when 0, and only Tifa takes more
	Conns          int           // requests in flight when not the same as Procs, 0 for Procs
	UniqueURLs     bool          // add a query to every timed read that no other read has, so that no two requests share a render
	Log            io.Writer
}

// Measure runs the measurement. Prepare must have run.
func (e Env) Measure(ctx context.Context, o Options) (*Record, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Rounds < 1 {
		return nil, fmt.Errorf("rounds must be at least 1")
	}
	if o.AcceptEncoding == "" {
		o.AcceptEncoding = "gzip"
	}
	if o.AcceptEncoding != "gzip" && o.AcceptEncoding != "identity" {
		return nil, fmt.Errorf("accept-encoding must be gzip or identity, not %q", o.AcceptEncoding)
	}
	if o.Cold == 0 {
		o.Cold = 1000
	}
	if len(o.Configs) == 0 {
		o.Configs = AllConfigs
	}
	if len(o.Scenarios) == 0 {
		o.Scenarios = []Scenario{ReadScenario}
	}
	var topology = SysTopology()
	placement, err := PlacementFor(runtime.GOOS, topology, o.ServerCPUs, o.LoadCPUs)
	if err != nil {
		return nil, err
	}
	if o.LoadCPUs != "" {
		if err := pinSelf(o.LoadCPUs); err != nil {
			return nil, err
		}
	}
	if o.Procs < 0 {
		return nil, fmt.Errorf("procs must be at least 1")
	}
	var specs []Spec
	for _, s := range e.Specs() {
		if len(o.Servers) > 0 && !slices.Contains(o.Servers, s.Name) {
			continue
		}
		if slices.ContainsFunc(s.Configs, func(c string) bool { return slices.Contains(o.Configs, c) }) {
			if s, err = s.WithProcs(o.Procs); err != nil {
				return nil, err
			}
			specs = append(specs, s)
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("no server matches configurations %v and servers %v", o.Configs, o.Servers)
	}
	rec := &Record{
		Date:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Versions:  e.versions(specs),
		Machine:   machine(),
		Placement: placement,
		Settings:  Settings{Rounds: o.Rounds, Steady: o.Steady.String(), Cold: o.Cold, Configs: o.Configs, Scenarios: scenarioNames(o.Scenarios), AcceptEncoding: o.AcceptEncoding, Procs: o.Procs, Conns: o.Conns, UniqueURLs: o.UniqueURLs},
	}
	fmt.Fprintf(o.Log, "placement: %s\n", placement.Note)
	if o.Procs > 1 {
		fmt.Fprintf(o.Log, "tifa: %d VMs\n", o.Procs)
	}
	if o.Conns > 0 {
		fmt.Fprintf(o.Log, "%d requests in flight\n", o.Conns)
	}
	for round := 0; round < o.Rounds; round++ {
		var order []planned
		if o.AA {
			for _, s := range Rotate(e.AASpecs(), round) {
				s, _ = s.WithProcs(o.Procs)
				order = append(order, planned{s, ReadScenario})
			}
		}
		for _, sc := range o.Scenarios {
			for _, s := range Rotate(sc.pick(specs), round) {
				order = append(order, planned{s, sc})
			}
		}
		for _, p := range order {
			spec := p.spec
			if err := waitIdle(ctx, o.Log, o.ServerCPUs+","+o.LoadCPUs); err != nil {
				return nil, err
			}
			run, err := runOnce(ctx, o, spec, p.scenario, round+1)
			if err != nil {
				return nil, err
			}
			rec.Runs = append(rec.Runs, *run)
			m := run.Metrics
			fmt.Fprintf(o.Log, "round %d %-10s %-15s first answer %5.0f ms  %7.1f req/s  P50 %6.2f  P99 %6.2f  max %6.1f ms  settles at %s  RSS %3.0f/%3.0f MiB", round+1, p.scenario.Name, spec.Name, m.FirstAnswer, m.Requests, m.P50, m.P99, m.Max, defByKey("settle").Format(m.Settle), m.RSSStart, m.RSSPeak)
			if run.Renders != nil {
				fmt.Fprintf(o.Log, "  hit rate %.1f%%", m.HitRate*100)
			}
			fmt.Fprintln(o.Log)
		}
	}
	rec.AA = AASpread(rec.Runs)
	if o.Render {
		render, err := e.renderBench(ctx, o.ServerCPUs)
		if err != nil {
			return nil, err
		}
		rec.Render = render
	}
	return rec, nil
}

// runOnce starts a server, times it from the start of its process, and stops it.
func runOnce(ctx context.Context, o Options, spec Spec, sc Scenario, round int) (*Run, error) {
	t0 := time.Now()
	server, err := spec.Launch(o.ServerCPUs)
	if err != nil {
		return nil, err
	}
	defer server.Stop()
	if err := server.WaitListening(ctx); err != nil {
		return nil, err
	}
	// RSS after start: the server listens and has answered nothing.
	rssStart := float64(server.RSS()) / (1 << 20)
	ae := o.AcceptEncoding
	if spec.AcceptEncoding != "" {
		ae = spec.AcceptEncoding
	}
	run := &Run{Spec: spec.Name, Framework: spec.Framework, Label: spec.Label, Configs: spec.Configs, Reference: spec.Reference, AA: slices.Contains(spec.Configs, ConfigAA), Round: round, Scenario: sc.Name}
	d, err := driveConns(ctx, server.Base, ae, spec.Name, server.Output, spec.Write, sc, o, t0)
	if err != nil {
		return nil, err
	}
	// The cold series is the first connection's, the steady phase is all of them.
	first := d.Conns[0]
	run.Metrics.FirstAnswer = float64(slices.MinFunc(d.Conns, func(a, b *connRun) int { return int(a.First - b.First) }).First) / float64(time.Millisecond)
	run.Wire = d.Wire
	steady, steadyPage := d.Steady, d.SteadyPage
	m := &run.Metrics
	m.RSSStart = rssStart
	run.Pages = pageStats(steady, steadyPage)
	if len(steady) > 0 {
		sorted := slices.Sorted(slices.Values(steady))
		m.Requests = d.Requests
		m.P50, m.P99, m.Max = Percentile(sorted, 50), Percentile(sorted, 99), sorted[len(sorted)-1]
	}
	if len(d.Writes) > 0 {
		sorted := slices.Sorted(slices.Values(d.Writes))
		m.Writes, m.WriteP50, m.WriteP99 = len(sorted), Percentile(sorted, 50), Percentile(sorted, 99)
	}
	m.First100 = mean(first.Cold[:min(100, len(first.Cold))])
	m.First1000 = mean(first.Cold[:min(1000, len(first.Cold))])
	m.Settle = settleOf(first.Cold, first.ColdPage, steady, steadyPage)
	m.RSSPeak = float64(server.PeakRSS()) / (1 << 20)
	if hits, total := d.Renders["cache"]+d.Renders["shared"], d.Renders["vm"]+d.Renders["cache"]+d.Renders["shared"]; total > 0 {
		m.HitRate = float64(hits) / float64(total)
		run.Renders = d.Renders
	}
	return run, nil
}

// settleOf compares each cold request with the steady latency (the median) of its page.
func settleOf(cold []float64, coldPage []int, steady []float64, steadyPage []int) float64 {
	byPage := map[int][]float64{}
	for i, l := range steady {
		byPage[steadyPage[i]] = append(byPage[steadyPage[i]], l)
	}
	want := make([]float64, len(cold))
	for i, p := range coldPage {
		if len(byPage[p]) == 0 {
			return SettleNever
		}
		want[i] = Median(byPage[p])
	}
	if at := Settle(cold, want); at > 0 {
		return float64(at)
	}
	return SettleNever
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

type client struct {
	http *http.Client
	base string
	ae   string // the Accept-Encoding of every request
}

// answer is what a request got: how long it took, X-Tifa-Render, and the body as it came over the
// connection (not decoded) with its Content-Encoding.
type answer struct {
	Latency  time.Duration
	Render   string
	Bytes    int64
	Encoding string
}

// newClient makes a client with one connection, no retries and no decoding of the body: it asks for
// acceptEncoding (gzip or identity), reads all of the answer and counts its bytes.
func newClient(base, acceptEncoding string) *client {
	return &client{base: base, ae: acceptEncoding, http: &http.Client{Transport: &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: 1, MaxConnsPerHost: 1}}}
}

// do sends a step and returns how long it took until its body was read, and its X-Tifa-Render.
func (c *client) do(ctx context.Context, st Step) (answer, error) {
	var body io.Reader
	if st.Body != nil {
		body = strings.NewReader(string(st.Body))
	}
	req, err := http.NewRequestWithContext(ctx, st.Method, c.base+st.Path, body)
	if err != nil {
		return answer{}, err
	}
	req.Header.Set("Accept-Encoding", c.ae)
	if st.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	start := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		return answer{}, err
	}
	n, err := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	a := answer{Latency: time.Since(start), Render: res.Header.Get("X-Tifa-Render"), Bytes: n, Encoding: res.Header.Get("Content-Encoding")}
	if err != nil {
		return answer{}, err
	}
	if res.StatusCode != 200 {
		return answer{}, fmt.Errorf("status %d", res.StatusCode)
	}
	if c.ae == "identity" && a.Encoding != "" {
		return answer{}, fmt.Errorf("Content-Encoding %s to Accept-Encoding: identity", a.Encoding)
	}
	return a, nil
}

// pinSelf puts this process, the client, on cpus (Linux). The client then needs no more threads
// than cpus.
func pinSelf(cpus string) error {
	list, err := ParseCPUList(cpus)
	if err != nil {
		return err
	}
	if out, err := exec.Command("taskset", "-a", "-c", "-p", cpus, strconv.Itoa(os.Getpid())).CombinedOutput(); err != nil {
		return fmt.Errorf("pin the client to cpus %s: %w\n%s", cpus, err, out)
	}
	runtime.GOMAXPROCS(len(list))
	return nil
}

// waitIdle waits, on Linux, until the given cpus have used under 0.1 of a CPU for 3 seconds, so
// that a server does not start next to the tail of the last one. Other platforms wait 3 seconds.
func waitIdle(ctx context.Context, log io.Writer, cpus string) error {
	if runtime.GOOS != "linux" || strings.Trim(cpus, ",") == "" {
		return sleep(ctx, 3*time.Second)
	}
	want := map[string]bool{}
	list, _ := ParseCPUList(strings.Trim(cpus, ","))
	for _, c := range list {
		want["cpu"+strconv.Itoa(c)] = true
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

// versions collects what the numbers depend on: the toolchain, the engine, Node, Tifa's commit
// and the version of each framework package of the specs.
func (e Env) versions(specs []Spec) map[string]string {
	v := map[string]string{"go": runtime.Version()}
	if data, err := os.ReadFile(e.Root + "/go.mod"); err == nil {
		if f, err := modfile.ParseLax("go.mod", data, nil); err == nil {
			for _, r := range f.Require {
				if r.Mod.Path == "github.com/liustack/quickjs" {
					v["quickjs"] = r.Mod.Version
				}
			}
		}
	}
	if out, err := exec.Command("node", "--version").Output(); err == nil {
		v["node"] = strings.TrimSpace(string(out))
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = e.Root
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}
	if rev := git("rev-parse", "--short", "HEAD"); rev != "" {
		if git("status", "--porcelain", "--untracked-files=no") != "" {
			rev += "-dirty"
		}
		v["tifa"] = rev
	}
	for _, s := range specs {
		if s.Version != "" {
			if ver := packageVersion(s.Dir + "/" + s.Version); ver != "" {
				v[s.Framework] = ver
			}
		}
	}
	return v
}

func machine() Machine {
	m := Machine{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		m.Kernel = strings.TrimSpace(string(out))
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				m.CPU = strings.TrimSpace(v)
				break
			}
		}
	} else if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		m.CPU = strings.TrimSpace(string(out))
	}
	return m
}

// Send sends one step to a server and fails unless it answers 200. It is for the writes of the
// tests, which check that a server takes them.
func Send(ctx context.Context, base string, st Step) error {
	_, err := newClient(base, "identity").do(ctx, st)
	return err
}
