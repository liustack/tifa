package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// emitApps are the apps whose pages TestEngineTagsAreReacts renders: where it starts and how many
// pages it follows links to. The shop's seeds include pages it answers with a 404.
var emitApps = []struct {
	name, dir string
	seeds     []string
	extreme   bool
}{
	{"shop", filepath.Join("examples", "shop"), []string{"/", "/projects/studio", "/projects/weekend", "/projects/studio/orders/SH-1042",
		"/projects/weekend/orders/WK-0201", "/projects/studio/stats", "/projects/weekend/stats", "/projects/nope", "/nothing-here",
		"/store", "/zh-CN/store", "/zh-CN/store/xx", "/store/journal", "/zh-CN/store/journal/stock-sync-api",
		"/store/products?page=2", "/store/products?color=moss&sort=price-asc", "/zh-CN/store/products",
		"/store/products/pour-over-kettle", "/zh-CN/store/products/pour-over-kettle", "/store/cart",
		"/store/checkout", "/zh-CN/store/cart", "/zh-CN/store/products/nope"}, false},
	{"mui", filepath.Join("test", "apps", "mui"), []string{"/", "/orders", "/orders?page=1", "/orders/ORD-1240", "/orders/nope"}, false},
	{"bench", filepath.Join("test", "apps", "bench"), []string{"/table", "/spiral"}, false},
	{"extreme", filepath.Join("test", "apps", "extreme"), nil, true},
}

// emitPages is how many pages of an app the test renders at most.
const emitPages = 160

var (
	emitLink  = regexp.MustCompile(`href="(/[^"#]*)"`)
	emitAsset = regexp.MustCompile(`^/(?:assets|public)/|\.[a-z0-9]+$`)
	// emitStamp matches the times the shop's loaders write into a page, RFC 3339 text that
	// differs between two runs.
	emitStamp  = regexp.MustCompile(`20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
	emitCounts = regexp.MustCompile(`tifa emit check: (\d+) compared, (\d+) the same, (\d+) declined, (\d+) differ`)
)

// The start and end tags that the engine writes in place of React DOM's functions (see
// docs/build.md) are React's own. Each app is built twice: with the test hook of the tags
// (TIFA_EMIT_CHECK=1 and the Go build tag tifa_emit_check), in which React writes every page while
// the engine writes each tag again to compare, and as it is deployed, in which the engine writes
// the tags. Every page of the two programs must be the same byte for byte, but for the shop's times
// and the build ID, and the program of the test hook must report no tag that differs. The pages
// are the seeds of each app and the pages their links lead to, and for test/apps/extreme, with
// TIFA_EXTREME=1, every size that renders.
func TestEngineTagsAreReacts(t *testing.T) {
	skipUnlessEnabled(t)
	t.Setenv("SHOP_OPEN", "1")
	for _, app := range emitApps {
		t.Run(app.name, func(t *testing.T) {
			if app.extreme && os.Getenv("TIFA_EXTREME") != "1" {
				t.Skip("extreme scenarios, set TIFA_EXTREME=1 as well to run them")
			}
			dir := filepath.Join(repoRoot(t), app.dir)
			if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
				t.Skipf("run pnpm install --frozen-lockfile in %s first", app.dir)
			}
			seeds := app.seeds
			if app.extreme {
				for _, page := range extremePages {
					if page.want != fails {
						seeds = append(seeds, page.url)
					}
				}
			}
			checked, checkedID := buildEmitCheck(t, dir)
			deployed := buildAt(t, dir)
			deployedID := buildID(t, dir)
			// Pages that near the render timeout render in either program however fast the machine.
			checkBase, checkCmd, checkOutput := start(t, checked, "--ssr-timeout", slowTimeout.String())
			base, _, _ := start(t, deployed, "--ssr-timeout", slowTimeout.String())

			seen := map[string]bool{}
			queue := append([]string(nil), seeds...)
			pages := 0
			for len(queue) > 0 && len(seen) < emitPages {
				url := queue[0]
				queue = queue[1:]
				if seen[url] {
					continue
				}
				seen[url] = true
				status, _, body := get(t, base+url)
				checkStatus, _, checkBody := get(t, checkBase+url)
				if status != checkStatus {
					t.Errorf("%s: %d with the engine's tags, %d with React's", url, status, checkStatus)
					continue
				}
				a := emitStamp.ReplaceAllString(strings.ReplaceAll(body, deployedID, "BUILD"), "TIME")
				b := emitStamp.ReplaceAllString(strings.ReplaceAll(checkBody, checkedID, "BUILD"), "TIME")
				if a != b {
					t.Errorf("%s: the page differs with the engine's tags:\n%s", url, firstDifference(a, b))
				}
				pages++
				if !app.extreme {
					for _, m := range emitLink.FindAllStringSubmatch(body, -1) {
						if link := strings.ReplaceAll(m[1], "&amp;", "&"); !seen[link] && !emitAsset.MatchString(link) && len(queue) < 400 {
							queue = append(queue, link)
						}
					}
				}
			}
			if err := checkCmd.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			if err := checkCmd.Wait(); err != nil {
				t.Errorf("the program of the test hook did not stop cleanly: %v", err)
			}
			<-checkOutput.drained
			out := checkOutput.String()
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "tifa emit check: differ") || strings.HasPrefix(line, "  C:") || strings.HasPrefix(line, "  React:") {
					t.Error(line)
				}
				if strings.HasPrefix(line, "tifa emit check: declined") {
					t.Log(line)
				}
			}
			counts := emitCounts.FindAllStringSubmatch(out, -1)
			if len(counts) == 0 {
				t.Fatalf("the program of the test hook compared no tags:\n%.4000s", out)
			}
			last := counts[len(counts)-1]
			if last[4] != "0" || last[2] == "0" {
				t.Errorf("tags of the engine: %s", last[0])
			}
			t.Logf("%d pages the same with the engine's tags. %s", pages, strings.TrimPrefix(last[0], "tifa emit check: "))
		})
	}
}

// buildEmitCheck builds the app in dir with the test hook of the start and end tags, and returns the
// program and its build ID. The build leaves its bundle in .tifa, so build the app as it is deployed
// after it.
func buildEmitCheck(t *testing.T, dir string) (exe, id string) {
	t.Helper()
	flags := os.Getenv("GOFLAGS")
	t.Setenv("TIFA_EMIT_CHECK", "1")
	t.Setenv("GOFLAGS", strings.TrimSpace(flags+" -tags=tifa_emit_check"))
	exe = buildAt(t, dir)
	os.Unsetenv("TIFA_EMIT_CHECK")
	os.Setenv("GOFLAGS", flags)
	ssr, err := os.ReadFile(filepath.Join(dir, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ssr), "__tifa_emit_check") {
		t.Fatal("the bundle of the test hook does not compare the tags")
	}
	return exe, buildID(t, dir)
}

// firstDifference shows where two pages first differ.
func firstDifference(engine, react string) string {
	i := 0
	for i < len(engine) && i < len(react) && engine[i] == react[i] {
		i++
	}
	from := max(0, i-200)
	return fmt.Sprintf("at %d\nengine: %.400s\nReact:  %.400s", i, engine[from:], react[from:])
}
