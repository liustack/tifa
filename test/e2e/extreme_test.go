package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// outcome is what a size of an extreme scenario must end in with the default settings.
type outcome int

const (
	renders outcome = iota // 200 from the VM, with the HTML the size asks for
	fails                  // 500 within ssr.timeout and a second, and the process goes on serving
	either                 // within a factor of two of a limit, so the speed of the machine decides
)

// extremePages are the sizes of each scenario of test/apps/extreme and texts their HTML must have.
// The sizes are those of the table in docs/testing.md, which also says why each ends as it does.
var extremePages = []struct {
	url   string
	want  outcome
	texts []string
}{
	{"/dom/10000", renders, []string{`id="files">`}},
	{"/dom/50000", renders, []string{`id="files">`}},
	{"/dom/100000", renders, []string{`id="files">`}},
	{"/nest/500", renders, []string{`data-level="500"`, `id="leaf"`}},
	{"/nest/1000", renders, []string{`data-level="1000"`, `id="leaf"`}},
	{"/nest/5000", renders, []string{`data-level="5000"`, `id="leaf"`}},
	{"/nest/10000", renders, []string{`data-level="10000"`, `id="leaf"`}},
	{"/deep/400", renders, []string{`id="bottom"`}},
	{"/deep/600", renders, []string{`id="bottom"`}},
	{"/inject/1", renders, []string{`id="items"`}},
	{"/inject/5", renders, []string{`id="items"`}},
	{"/inject/20", fails, nil},
	{"/json/1999", renders, []string{`id="levels">1998<`}},
	{"/json/2001", jsonPast2000, []string{`id="levels">2000<`}},
	{"/table/1000", renders, []string{`SH-200999`}},
	{"/table/5000", fails, nil},
	{"/table/10000", fails, nil},
	{"/providers/200", renders, []string{`id="sum">20100<`}},
	{"/providers/500", renders, []string{`id="sum">125250<`}},
	{"/providers/1000", renders, []string{`id="sum">500500<`}},
	{"/charts/12", renders, []string{`<circle`}},
	{"/markdown/200", either, []string{`id="blocks">50<`}},
	{"/markdown/1000", fails, nil},
	{"/markdown/2000", fails, nil},
	{"/i18n/3000", renders, []string{`id="formats"`}},
	{"/mall/zh-CN", renders, []string{`id="title"`, `id="price"`}},
	{"/mall/en-US", renders, []string{`id="title"`, `id="price"`}},
	{"/console/100", either, []string{`id="comments">300<`}},
}

// extremeRSS is the most memory the program may hold at once over all the sizes: its two VMs
// at their 128 MiB limit, and the data, HTML and compressed copy of the largest page in Go.
const extremeRSS = 1 << 30

// TestExtremeApp builds test/apps/extreme and requests each size of each scenario in turn. A size
// that renders must render all of its data. A size that fails must fail as a 500 within the time
// budget and leave the program serving the next request. It is slow and needs about 1 GiB, so it
// runs only with TIFA_EXTREME=1 as well.
func TestExtremeApp(t *testing.T) {
	skipUnlessEnabled(t)
	if os.Getenv("TIFA_EXTREME") != "1" {
		t.Skip("extreme scenarios, set TIFA_EXTREME=1 as well to run them")
	}
	dir := filepath.Join(repoRoot(t), "test", "apps", "extreme")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/extreme first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)
	base, cmd, output := start(t, exe)
	t.Cleanup(func() {
		if t.Failed() {
			out := output.String()
			t.Logf("the end of the output of the program:\n%s", out[max(0, len(out)-8000):])
		}
	})

	index := renderedPage(t, base+"/")
	expectInBody(t, "/", index, `href="/dom/10000"`, `href="/console/100"`)
	const budget = 3*time.Second + time.Second
	for _, page := range extremePages {
		begin := time.Now()
		status, header, body := get(t, base+page.url)
		took := time.Since(begin)
		t.Logf("%s: %d in %s, %d KiB", page.url, status, took.Round(time.Millisecond), len(body)>>10)
		switch {
		case status == 200 && page.want != fails:
			if header.Get("X-Tifa-Render") != "vm" {
				t.Errorf("%s: rendered by %q, want the VM", page.url, header.Get("X-Tifa-Render"))
			}
			expectInBody(t, page.url, body, page.texts...)
		case status == 500 && page.want != renders:
			if took > budget {
				t.Errorf("%s: failed after %s, want within %s", page.url, took, budget)
			}
			// The VM that failed is replaced, and the next page renders.
			renderedPage(t, base+"/")
		default:
			t.Errorf("%s: %d, want %s:\n%.600s", page.url, status, page.want, body)
		}
		select {
		case <-output.drained: // the output ends when the program exits
			t.Fatalf("the program exited after %s:\n%s", page.url, output.String())
		default:
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the program did not stop cleanly: %v\n%s", err, output.String())
	}
	if strings.Contains(output.String(), "panic") {
		t.Errorf("the program panicked:\n%s", output.String())
	}
	peak, ok := peakRSS(cmd)
	if !ok {
		return
	}
	t.Logf("the program held at most %.0f MiB", float64(peak)/(1<<20))
	if peak > extremeRSS {
		t.Errorf("the program held %.0f MiB at once, more than %d MiB", float64(peak)/(1<<20), extremeRSS>>20)
	}
}

func (o outcome) String() string {
	return [...]string{"200 from the VM", "500 within the budget", "200 or 500"}[o]
}
