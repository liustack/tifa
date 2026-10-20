package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var firstCell = regexp.MustCompile(`<td>([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})</td>`)

// test/apps/bench holds two pages of public React SSR benchmarks: the 1000-row table of
// eknkc/ssr-benchmark, with its rows from a Load, and the tile spiral of
// platformatic/ssr-performance-showdown, computed during the render.
func TestBenchApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "bench")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/bench first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)
	base, _, _ := start(t, exe)

	body := renderedPage(t, base+"/table")
	if n := strings.Count(body, "<tr>"); n != 1000 {
		t.Errorf("/table has %d rows, want 1000", n)
	}
	// The rows also reach the page as the Load's data, for the browser to take over with.
	if m := firstCell.FindStringSubmatch(body); m == nil || strings.Count(body, m[1]) < 2 {
		t.Errorf("/table does not carry its first row %v as data", m)
	}

	body = renderedPage(t, base+"/spiral")
	if n := strings.Count(body, `class="tile"`); n != 2398 {
		t.Errorf("/spiral has %d tiles, want 2398", n)
	}
	expectInBody(t, "/spiral", body, `<div id="wrapper">`, `style="left:480.00px;top:360.00px"`)
}
