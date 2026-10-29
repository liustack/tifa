package compare_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The tests build three apps and run them, so they are off unless TIFA_COMPARE=1. They need go,
// node and pnpm and are skipped without them. TIFA_COMPARE_SKIP_BUILD=1 uses what the last run
// built. None of this is part of CI.
//
//	TIFA_COMPARE=1 go test -count=1 -v ./test/compare

func setup(t *testing.T) compare.Env {
	t.Helper()
	if os.Getenv("TIFA_COMPARE") != "1" {
		t.Skip("builds three apps with Node, set TIFA_COMPARE=1 to run it")
	}
	if why := compare.MissingTools(); why != "" {
		t.Skip(why)
	}
	env, err := compare.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	return env
}

var (
	shopOnce, appsOnce sync.Once
	shopErr, appsErr   error
	shopLog, appsLog   strings.Builder
)

// buildShop builds bin/shop once for all the tests.
func buildShop(t *testing.T, env compare.Env) {
	t.Helper()
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") == "1" {
		return
	}
	shopOnce.Do(func() { shopErr = env.BuildShop(context.Background(), &shopLog) })
	if shopErr != nil {
		t.Fatalf("%v\n%s", shopErr, &shopLog)
	}
}

// buildApps builds the shop and the two Node apps once for all the tests.
func buildApps(t *testing.T, env compare.Env) {
	t.Helper()
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") == "1" {
		return
	}
	appsOnce.Do(func() { appsErr = env.Prepare(context.Background(), &appsLog) })
	if appsErr != nil {
		t.Fatalf("%v\n%s", appsErr, &appsLog)
	}
}

// TestDomainMatchesGo checks that the TypeScript port of the shop's domain code computes what the
// Go code returns. For each of the six pages it compares, field by field, the JSON that the Go
// Layout and Load functions answer with the JSON that Node makes from the port. When the Go code
// of the shop changes and the port does not, this fails and names the first fields that differ.
// It needs no build of the two Node apps: Node 22.18 and later run the port as it is.
func TestDomainMatchesGo(t *testing.T) {
	env := setup(t)
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	buildShop(t, env)
	ctx := context.Background()
	want, err := env.GoData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.NodeData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantAny, gotAny := toAny(t, want), toAny(t, got)
	diffs := compare.DiffData(wantAny, gotAny, 20)
	for _, d := range diffs {
		t.Error(d)
	}
	if len(diffs) > 0 {
		t.Error("the TypeScript port in test/compare/domain no longer matches the Go code of examples/shop: port the change")
	}
	for _, p := range compare.Pages {
		if len(want[p.URL]) == 0 || len(got[p.URL]) != len(want[p.URL]) {
			t.Errorf("%s: Go answered %d routes, Node %d", p.URL, len(want[p.URL]), len(got[p.URL]))
		}
	}
}

func toAny(t *testing.T, v compare.PageData) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPagesShowTheSameText builds the shop as a tifa program and as a Next.js and a TanStack Start
// app, and checks that the six workbench pages show the same title and text, with as many table
// rows, links, SVGs, buttons, times and photos.
func TestPagesShowTheSameText(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	views := map[string][]compare.View{}
	var names []string
	for _, target := range env.Targets() {
		if target.Name == "nextjs-jitless" {
			continue // the same app and the same HTML as nextjs
		}
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range compare.Pages {
			body, err := compare.Fetch(server.Base, page)
			if err != nil {
				t.Errorf("%s: %v", target.Name, err)
				views[target.Name] = append(views[target.Name], compare.View{})
				continue
			}
			views[target.Name] = append(views[target.Name], compare.ParseView(body))
		}
		server.Stop()
		names = append(names, target.Name)
	}
	tifa := views["tifa"]
	for _, name := range names[1:] {
		for i, page := range compare.Pages {
			if len(views[name]) <= i || len(tifa) <= i {
				continue
			}
			if diff := compare.Diff(tifa[i], views[name][i]); diff != "" {
				t.Errorf("%s, %s differs from tifa:\n%s", page.Name, name, diff)
			}
		}
	}
	for i, page := range compare.Pages {
		t.Logf("%-14s %2d text nodes, tifa %d bytes, Next.js %d, Start %d", page.Name, len(tifa[i].Text), tifa[i].Bytes, views["nextjs"][i].Bytes, views["start"][i].Bytes)
	}
}

// requests is how many times the test asks for each page after the first.
const requests = 5

// TestEveryRequestComputesAndRenders checks that each request for a page runs the data code and
// renders the page again, by counting instead of by reading the configuration. In each server it
// asks for a page once and reads what the counters gained, then asks again several times and
// checks that they gained that many times more.
//
//   - tifa is the shop built with a counter on ordersOf, the query every page's data starts with
//     (see Env.BuildCounted), and renders are read from X-Tifa-Render, which says "vm" for a page
//     rendered in a VM and "cache" or "shared" for one that was not.
//   - The Node apps count ordersOf in the port, and the runs of the root Layout, the project
//     Layout and the page's Load, and the renders of the page on the server.
//
// The calls of ordersOf for one page must also be the same in all three: when they are, the Go
// code and the port query the orders the same number of times.
func TestEveryRequestComputesAndRenders(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	dir := t.TempDir()
	exe, err := env.BuildCounted(ctx, io.Discard, dir)
	if err != nil {
		t.Fatal(err)
	}
	targets := env.Targets()
	tifa := targets[0]
	tifa.Prog = exe

	perPage := map[string]map[string]int64{} // server -> page -> calls of ordersOf for one request
	for _, target := range []compare.Target{tifa, targets[1], targets[3]} {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		node := target.Name != "tifa"
		perPage[target.Name] = map[string]int64{}
		for _, page := range compare.Pages {
			before := readCounts(t, server.Base)
			render := fetchRender(t, server.Base, page)
			one := diffCounts(before, readCounts(t, server.Base))
			if one["ordersOf"] <= 0 {
				t.Errorf("%s, %s: one request made no query of the orders: %v", target.Name, page.Name, one)
			}
			perPage[target.Name][page.Name] = one["ordersOf"]
			if node {
				for key, want := range map[string]int64{"shell": 1, "project": 1, "page": 1, "renders": 1} {
					if one[key] != want {
						t.Errorf("%s, %s: one request ran %s %d times, want %d: %v", target.Name, page.Name, key, one[key], want, one)
					}
				}
			} else if render != "vm" {
				t.Errorf("%s, %s: X-Tifa-Render is %q, want vm", target.Name, page.Name, render)
			}

			before = readCounts(t, server.Base)
			for range requests {
				if r := fetchRender(t, server.Base, page); !node && r != "vm" {
					t.Errorf("%s, %s: X-Tifa-Render is %q, want vm", target.Name, page.Name, r)
				}
			}
			many := diffCounts(before, readCounts(t, server.Base))
			for key, n := range many {
				if n != requests*one[key] {
					t.Errorf("%s, %s: %d requests made %d of %s, want %d", target.Name, page.Name, requests, n, key, requests*one[key])
				}
			}
		}
		server.Stop()
	}
	for _, page := range compare.Pages {
		a, b, c := perPage["tifa"][page.Name], perPage["nextjs"][page.Name], perPage["start"][page.Name]
		if a != b || a != c {
			t.Errorf("%s: calls of ordersOf for one request: tifa %d, Next.js %d, Start %d", page.Name, a, b, c)
		}
		t.Logf("%-14s %d calls of ordersOf for a request in tifa, %d in Next.js, %d in Start", page.Name, a, b, c)
	}
}

// TestNoCachingBetweenRequests reads what the two Node apps would need to cache a page and finds
// none of it: no Next.js cacheComponents or "use cache", no cache header on a page of Start, no
// QueryClient on the server of Start.
func TestNoCachingBetweenRequests(t *testing.T) {
	env := setup(t)
	buildApps(t, env)

	for _, dir := range []string{env.Next, env.Start} {
		for _, bad := range []string{"cacheComponents", "use cache", "unstable_cache", "revalidate =", "QueryClient", "react-query", "staticGeneration"} {
			scan(t, dir, bad)
		}
	}
	// A page of Start has no header that lets a cache keep it. Next.js says it must not be kept.
	ctx := context.Background()
	targets := env.Targets()
	for _, target := range []compare.Target{targets[1], targets[3]} {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		_, header, err := compare.FetchWithHeader(server.Base, compare.Pages[2])
		server.Stop()
		if err != nil {
			t.Fatal(err)
		}
		cache := header.Get("Cache-Control")
		switch target.Name {
		case "nextjs":
			if !strings.Contains(cache, "no-store") {
				t.Errorf("Next.js answers a page with Cache-Control %q, want no-store", cache)
			}
		case "start":
			for _, h := range []string{"Cache-Control", "ETag", "Last-Modified", "Expires", "Age"} {
				if v := header.Get(h); v != "" {
					t.Errorf("Start answers a page with %s: %s", h, v)
				}
			}
		}
	}
}

// scan fails when a source of the app, or the file that lists its packages, holds text.
func scan(t *testing.T, app, text string) {
	t.Helper()
	for _, root := range []string{filepath.Join(app, "src"), filepath.Join(app, "shop")} {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
				return nil
			}
			if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), text) {
				t.Errorf("%s holds %q", path, text)
			}
			return nil
		})
	}
	for _, file := range []string{"package.json", "next.config.ts", "vite.config.ts"} {
		if data, err := os.ReadFile(filepath.Join(app, file)); err == nil && strings.Contains(string(data), text) {
			t.Errorf("%s holds %q", filepath.Join(app, file), text)
		}
	}
}

func readCounts(t *testing.T, base string) map[string]int64 {
	t.Helper()
	res, err := http.Get(base + "/compare-counts")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var counts map[string]int64
	if err := json.NewDecoder(res.Body).Decode(&counts); err != nil {
		t.Fatal(err)
	}
	return counts
}

func diffCounts(before, after map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for key, n := range after {
		out[key] = n - before[key]
	}
	return out
}

// fetchRender asks for a page and returns X-Tifa-Render, which only tifa sets.
func fetchRender(t *testing.T, base string, page compare.Page) string {
	t.Helper()
	_, header, err := compare.FetchWithHeader(base, page)
	if err != nil {
		t.Fatal(err)
	}
	return header.Get("X-Tifa-Render")
}
