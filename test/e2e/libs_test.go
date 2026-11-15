package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// dehydratedAt is the time TanStack Query dehydrated its state at, in milliseconds.
var dehydratedAt = regexp.MustCompile(`"dehydratedAt":\d+`)

// test/apps/libs has nine pages that each put a group of widely used libraries through the
// server render: immer with Redux Toolkit, recharts with d3, date-fns with dayjs, jsdiff,
// shadcn/ui on Radix, TanStack Query with TanStack Table, react-hook-form with zod, react-intl,
// and Motion (whose motion values read performance.now, which the VM provides).
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
	expectInBody(t, "/", body, "<title>Library pages</title>", `href="/state"`, `href="/chart"`, `href="/dates"`, `href="/diff"`,
		`href="/shadcn"`, `href="/query"`, `href="/forms"`, `href="/intl"`, `href="/motion"`)

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
		{
			// Forty shadcn components as the shadcn CLI writes them, with cva, tailwind-merge, Radix and
			// a react-day-picker calendar.
			"/shadcn",
			[]string{"<title>shadcn/ui · Library pages</title>"},
			map[string]int{`data-slot="button"`: 135, `data-slot="badge"`: 124},
		},
		{
			// Queries prefetched and dehydrated as a loader does, and a table of 300 orders.
			"/query",
			[]string{"<title>Query · Library pages</title>", "<h1>Orders", `id="query-state"`, "<h2>Needs attention"},
			nil,
		},
		{
			// A checkout that failed zod, with every issue shown next to its field, and an import
			// preview checked with safeParse.
			"/forms",
			[]string{"<title>Forms · Library pages</title>", "<h1>Checkout", "Import preview: <!-- -->93<!-- --> ready, <!-- -->57"},
			map[string]int{`role="alert"`: 14},
		},
		{
			// ICU messages in three languages. The VM writes plain spaces in dates and times, so none
			// of the facts holds one.
			"/intl",
			[]string{"<title>Intl · Library pages</title>", "<h2>48 stays in Kraków</h2>", "<h2>48 noclegów w Kraków</h2>", "Willkommen zurück, <b>Ada</b>! Sie hat 12.840 Punkte."},
			nil,
		},
		{
			// Motion elements whose initial styles are in the HTML. The motion values of the hero read
			// performance.now while rendering, and the page has no shim for it.
			"/motion",
			[]string{
				"<title>Motion · Library pages</title>",
				`<h1 style="letter-spacing:-0.04em;color:#111827">Ship faster</h1>`,
				`<article class="plan popular" style="opacity:0;transform:translateY(30px) rotateX(15deg)">`,
				"Choose <!-- -->Enterprise",
			},
			map[string]int{"<article": 4},
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
		// the clock or a random number would differ. The one clock read the pages make is the
		// dehydratedAt that TanStack Query writes into its state on /query, so it is blanked first.
		if again := renderedPage(t, other+p.path); dehydratedAt.ReplaceAllString(again, "") != dehydratedAt.ReplaceAllString(body, "") {
			t.Errorf("%s rendered two different pages", p.path)
		}
	}
}
