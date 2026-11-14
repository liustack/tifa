package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// test/apps/libs has four pages that each put a group of widely used libraries through the
// server render: immer with Redux Toolkit, recharts with d3, date-fns with dayjs, and jsdiff.
// Each page loads fixed data in Go and does the library work while it renders.
func TestLibsApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "libs")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/libs first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)
	base, _, _ := start(t, exe)
	other, _, _ := start(t, exe)

	body := renderedPage(t, base+"/")
	expectInBody(t, "/", body, "<title>Library pages</title>", `href="/state"`, `href="/chart"`, `href="/dates"`, `href="/diff"`)

	pages := []struct {
		path string
		want []string
		// counts maps a string to how many times the page holds it.
		counts map[string]int
	}{
		{
			// The store replayed 600 events over 320 tickets: the agent table, the open tickets and
			// the median of the closed ones come out of the reducers and selectors.
			"/state",
			[]string{
				"<title>State · Library pages</title>",
				"<td>Ada</td><td>22</td><td>3</td><td>105</td>",
				"<b>T5195</b>",
				"solved, median <!-- -->24<!-- -->h",
			},
			map[string]int{`<li class="prio-`: 146},
		},
		{
			// recharts draws its charts on the server, d3 makes the curves of the small charts.
			"/chart",
			[]string{
				"<title>Chart · Library pages</title>",
				`recharts-line-curve" d="M65,131.933C67.325,143.444`,
				"<title>web 24.9%</title>",
			},
			map[string]int{`<figure class="spark">`: 36, `recharts-line-curve" d=`: 4},
		},
		{
			"/dates",
			[]string{
				"<title>Dates · Library pages</title>",
				"<h2>July 2026</h2>",
				"<b>Monday, 1st June</b>",
				"about 2 months ago",
				"Mon 1 Jun 2026 wk 23, 1st",
				"<td>Ada</td><td>2026-W23</td>",
			},
			map[string]int{`<li class="`: 360},
		},
		{
			"/diff",
			[]string{
				"<title>Diff · Library pages</title>",
				"<h1>Shop handbook<del>,</del><ins>:</ins> <ins>four</ins>th<del>ird</del> edition<ins> (draft)</ins></h1>",
				`<span class="add">    &quot;grace&quot;`,
			},
			map[string]int{`<tr class="hunk">`: 98},
		},
	}
	for _, p := range pages {
		body := renderedPage(t, base+p.path)
		expectInBody(t, p.path, body, p.want...)
		for s, want := range p.counts {
			if n := strings.Count(body, s); n != want {
				t.Errorf("%s has %d of %s, want %d", p.path, n, s, want)
			}
		}
		// Another program has an empty render memo, so it renders the page again. A page that read
		// the clock or a random number would differ.
		if again := renderedPage(t, other+p.path); again != body {
			t.Errorf("%s rendered two different pages", p.path)
		}
	}
}
