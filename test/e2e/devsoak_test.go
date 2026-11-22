package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// devSoakAppGrowth is how much the app's RSS may grow, in a stretch of samples that share one
	// app process, between its first sample at or after devSoakWarmup and its last. It is loose
	// until real data from long runs sets it.
	devSoakAppGrowth = 0.5
	// devSoakToolGrowth is the same limit for tifa dev and Vite, as a factor of their RSS at
	// devSoakWarmup.
	devSoakToolGrowth = 2.0
	devSoakWarmup     = 20
)

// soakSample is one row of the CSV.
type soakSample struct {
	i         int
	kind      string
	rebuild   time.Duration
	dev, vite int // RSS in KiB
	app       int
	appPID    int
	appAnon   int // KiB, Linux only
}

type psRow struct {
	pid, ppid, rss int
	command        string
}

func psTable(t *testing.T) []psRow {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var rows []psRow
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		rss, _ := strconv.Atoi(f[2])
		rows = append(rows, psRow{pid, ppid, rss, strings.Join(f[3:], " ")})
	}
	return rows
}

// anonKiB reads RssAnon of a process on Linux, and 0 elsewhere.
func anonKiB(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "RssAnon:" {
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return 0
}

// soakProcesses finds Vite's node process and the app below the tifa dev process.
func soakProcesses(t *testing.T, devPID int, dir string) (dev, vite, app psRow) {
	t.Helper()
	rows := psTable(t)
	children := func(pid int) (out []psRow) {
		for _, r := range rows {
			if r.ppid == pid {
				out = append(out, r)
			}
		}
		return out
	}
	for _, r := range rows {
		if r.pid == devPID {
			dev = r
		}
	}
	if dev.pid == 0 {
		t.Fatalf("tifa dev (pid %d) is gone", devPID)
	}
	for _, r := range children(devPID) {
		switch {
		case filepath.Base(strings.Fields(r.command)[0]) == "node":
			vite = r
		case strings.Contains(r.command, dir) && !strings.Contains(r.command, "go build"):
			app = r
		}
	}
	if vite.pid == 0 || app.pid == 0 {
		t.Fatalf("children of tifa dev: vite %+v, app %+v\n%v", vite, app, children(devPID))
	}
	return dev, vite, app
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// TestDevManyRebuilds edits a dev app many times and samples the memory of the three processes of
// tifa dev: tifa dev, Vite's node, and the app. Edits 1, 6, 11 and so on change Go code (go build, then
// the app restarts), the others change a page (the app loads the new bundle in place).
// TIFA_DEV_SOAK=1 turns it on, TIFA_DEV_SOAK_N sets the count (200), TIFA_DEV_SOAK_OUT names the
// CSV.
func TestDevManyRebuilds(t *testing.T) {
	skipUnlessEnabled(t)
	if os.Getenv("TIFA_DEV_SOAK") != "1" {
		t.Skip("long test, set TIFA_DEV_SOAK=1 to run it")
	}
	n := 200
	if v := os.Getenv("TIFA_DEV_SOAK_N"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("TIFA_DEV_SOAK_N=%q", v)
		}
	}
	out := os.Getenv("TIFA_DEV_SOAK_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "soak.csv")
	}

	dir := devApp(t)
	d := startDev(t, dir)
	go func() { // keep the status channel from filling up
		for range d.statuses {
		}
	}()
	d.waitLog(t, 0, ready)
	expectBody(t, d.base+"/", "North shop")

	pageText, goText := "North shop", "Shop "
	var samples []soakSample
	for i := 1; i <= n; i++ {
		kind := "page"
		if i%5 == 1 {
			kind = "go"
		}
		start := time.Now()
		var mark int
		var want, url string
		if kind == "page" {
			next := fmt.Sprintf("North v%d", i)
			mark = d.edit(t, "app/page.tsx", pageText, next)
			pageText, want, url = next, next, d.base+"/"
		} else {
			next := fmt.Sprintf("Shop g%d ", i)
			mark = d.edit(t, "app/shops/shop+/route.go", `"`+goText+`" + id`, `"`+next+`" + id`)
			goText, want, url = next, next+"north", d.base+"/shops/north"
		}
		out := d.waitLog(t, mark, ready)
		if buildFailed.MatchString(out) {
			t.Fatalf("edit %d (%s) failed:\n%s", i, kind, out)
		}
		expectBody(t, url, want)
		took := time.Since(start)

		dev, vite, app := soakProcesses(t, d.cmd.Process.Pid, dir)
		samples = append(samples, soakSample{i, kind, took, dev.rss, vite.rss, app.rss, app.pid, anonKiB(app.pid)})
	}

	var csv strings.Builder
	csv.WriteString("i,kind,rebuild_ms,devRSS,viteRSS,appRSS,appPID,appAnon\n")
	for _, s := range samples {
		fmt.Fprintf(&csv, "%d,%s,%d,%d,%d,%d,%d,%d\n", s.i, s.kind, s.rebuild.Milliseconds(), s.dev, s.vite, s.app, s.appPID, s.appAnon)
	}
	if err := os.WriteFile(out, []byte(csv.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("samples (RSS in KiB): %s", out)

	at := func(i int) soakSample { return samples[min(i, len(samples))-1] }
	for _, i := range []int{devSoakWarmup, n / 2, n} {
		s := at(i)
		t.Logf("i=%d: dev %d, vite %d, app %d (pid %d, anon %d)", min(i, n), s.dev, s.vite, s.app, s.appPID, s.appAnon)
	}
	for _, kind := range []string{"page", "go"} {
		var took []time.Duration
		for _, s := range samples {
			if s.kind == kind {
				took = append(took, s.rebuild)
			}
		}
		if len(took) > 0 {
			t.Logf("%s rebuild: median %v, p90 %v (%d edits)", kind, percentile(took, 0.5), percentile(took, 0.9), len(took))
		}
	}

	if n <= devSoakWarmup {
		return
	}
	first, last := at(devSoakWarmup), at(n)
	if float64(last.vite) > devSoakToolGrowth*float64(first.vite) {
		t.Errorf("Vite RSS grew from %d to %d KiB", first.vite, last.vite)
	}
	if float64(last.dev) > devSoakToolGrowth*float64(first.dev) {
		t.Errorf("tifa dev RSS grew from %d to %d KiB", first.dev, last.dev)
	}
	// The app restarts on a Go edit, so compare inside each stretch that shares one process.
	for lo := devSoakWarmup - 1; lo < n; {
		hi := lo
		for hi+1 < n && samples[hi+1].appPID == samples[lo].appPID {
			hi++
		}
		if hi > lo && float64(samples[hi].app) > (1+devSoakAppGrowth)*float64(samples[lo].app) {
			t.Errorf("app RSS (pid %d) grew from %d KiB at i=%d to %d KiB at i=%d", samples[lo].appPID, samples[lo].app, samples[lo].i, samples[hi].app, samples[hi].i)
		}
		lo = hi + 1
	}
	// Also across restarts: the app at the end against the app at the warmup sample.
	if float64(last.app) > (1+devSoakAppGrowth)*float64(first.app) {
		t.Errorf("app RSS grew from %d KiB at i=%d to %d KiB at i=%d", first.app, first.i, last.app, last.i)
	}
}
