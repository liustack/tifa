package compare_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
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
		t.Skip("builds three Node apps, set TIFA_COMPARE=1 to run it")
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

// buildApps builds the shop and the three Node apps once for all the tests.
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
	for _, p := range compare.AllPages() {
		if len(want[p.URL]) == 0 || len(got[p.URL]) != len(want[p.URL]) {
			t.Errorf("%s: Go answered %d routes, Node %d", p.URL, len(want[p.URL]), len(got[p.URL]))
		}
	}
	if len(want) != len(compare.AllPages()) || len(got) != len(want) {
		t.Errorf("AllPages lists %d pages, Go answered %d, Node %d", len(compare.AllPages()), len(want), len(got))
	}
	// The orders AllPages assumes are the orders the shop has: every order counted by the lists has a page.
	for _, project := range []string{"studio", "weekend"} {
		list, _ := want["/projects/"+project]["app/projects/project+/route"].(map[string]any)
		rows, _ := list["orders"].([]any)
		total, _ := list["total"].(float64)
		r := compare.OrderIDs[project]
		if int(total) != r[1]-r[0]+1 || len(rows) != min(int(total), 25) {
			t.Errorf("%s: the shop has %v orders and sends %d, AllPages assumes %d and the first page holds 25", project, total, len(rows), r[1]-r[0]+1)
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

// TestPagesShowTheSameText builds the shop as a tifa program and as a Next.js, a TanStack Start and a React Router
// app, and checks that the six workbench pages show the same title and text, with as many table
// rows, links, SVGs, buttons, times and photos.
func TestPagesShowTheSameText(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	views := map[string][]compare.View{}
	var names []string
	for _, target := range env.Targets() {
		if strings.HasSuffix(target.Name, "-jitless") || target.Name == "tifa-nocache" {
			continue // the same app and the same HTML as the one without --jitless or with the cache
		}
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range compare.AllPages() {
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
		for i, page := range compare.AllPages() {
			if len(views[name]) <= i || len(tifa) <= i {
				continue
			}
			if diff := compare.Diff(tifa[i], views[name][i]); diff != "" {
				t.Errorf("%s, %s differs from tifa:\n%s", page.Name, name, diff)
			}
		}
	}
	for i, page := range compare.Pages {
		t.Logf("%-14s %2d text nodes, tifa %d bytes, Next.js %d, Start %d, React Router %d", page.Name, len(tifa[i].Text), tifa[i].Bytes, views["nextjs"][i].Bytes, views["start"][i].Bytes, views["reactrouter"][i].Bytes)
	}
}

// requests is how many times the test asks for each page after the first.
const requests = 5

// TestEveryRequestComputesAndRenders checks that each request for a page runs the data code and
// renders the page again, by counting instead of by reading the configuration. In each server it
// asks for a page once and reads what the counters gained, then asks again several times and
// checks that they gained that many times more.
//
//   - tifa is the shop built with a counter on ordersOf, which every read of the orders starts with
//     (see Env.BuildCounted), and renders are read from X-Tifa-Render, which says "vm" for a page
//     rendered in a VM and "memo" or "shared" for one that was not.
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
	tifa := mustTarget(t, env, "tifa-nocache")
	tifa.Prog = exe
	countsAddr := freeAddr(t)
	tifa.Env = append(tifa.Env, "COMPARE_COUNTS_ADDR="+countsAddr)
	storeCountsAddr := freeAddr(t)
	tifa.Env = append(tifa.Env, compare.StoreCountsEnv+"="+storeCountsAddr)
	perStore := map[string]map[string]int64{} // server -> storefront request -> calls of FindLocale

	perPage := map[string]map[string]int64{} // server -> page -> calls of ordersOf for one request
	for _, target := range []compare.Target{tifa, mustTarget(t, env, "nextjs"), mustTarget(t, env, "start"), mustTarget(t, env, "reactrouter")} {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		node := target.Name != "tifa-nocache"
		counts := server.Base
		if !node {
			counts = "http://" + countsAddr
		}
		perPage[target.Name] = map[string]int64{}
		for _, page := range compare.AllPages() {
			before := readCounts(t, counts)
			render := fetchRender(t, server.Base, page)
			one := diffCounts(before, readCounts(t, counts))
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

			before = readCounts(t, counts)
			for range requests {
				if r := fetchRender(t, server.Base, page); !node && r != "vm" {
					t.Errorf("%s, %s: X-Tifa-Render is %q, want vm", target.Name, page.Name, r)
				}
			}
			many := diffCounts(before, readCounts(t, counts))
			for key, n := range many {
				if n != requests*one[key] {
					t.Errorf("%s, %s: %d requests made %d of %s, want %d", target.Name, page.Name, requests, n, key, requests*one[key])
				}
			}
		}
		// The storefront: the same, for the pages of storeFairPages and a write.
		countBases := []string{counts}
		if !node {
			countBases = []string{counts, "http://" + storeCountsAddr}
		}
		perStore[target.Name] = storeComputes(t, target.Name, server.Base, countBases, node, storeWriteOf(target.Name))
		server.Stop()
	}
	for _, page := range compare.AllPages() {
		a, b, c, d := perPage["tifa-nocache"][page.Name], perPage["nextjs"][page.Name], perPage["start"][page.Name], perPage["reactrouter"][page.Name]
		if a != b || a != c || a != d {
			t.Errorf("%s: calls of ordersOf for one request: tifa %d, Next.js %d, Start %d, React Router %d", page.Name, a, b, c, d)
		}
		t.Logf("%-14s %d calls of ordersOf for a request in tifa, %d in Next.js, %d in Start, %d in React Router", page.Name, a, b, c, d)
	}
	for label, a := range perStore["tifa-nocache"] {
		b, c, d := perStore["nextjs"][label], perStore["start"][label], perStore["reactrouter"][label]
		if a != b || a != c || a != d {
			t.Errorf("%s: calls for one request: tifa %d, Next.js %d, Start %d, React Router %d", label, a, b, c, d)
		}
		t.Logf("%-52s %d calls in tifa, %d in Next.js, %d in Start, %d in React Router", label, a, b, c, d)
	}
}

// TestNoCachingBetweenRequests reads what the Node apps would need to cache a page and finds none
// of it: no Next.js cacheComponents or "use cache", no cache header on a page of Start or React
// Router, no QueryClient on the server of Start, no headers or clientLoader export in React Router.
func TestNoCachingBetweenRequests(t *testing.T) {
	env := setup(t)
	buildApps(t, env)

	for _, dir := range []string{env.Next, env.Start, env.React} {
		for _, bad := range []string{"cacheComponents", "use cache", "unstable_cache", "revalidate =", "QueryClient", "react-query", "staticGeneration", "clientLoader", "export const headers", "export function headers", "shouldRevalidate"} {
			scan(t, dir, bad)
		}
	}
	// A page of Start or React Router has no header that lets a cache keep it. Next.js says it must not be kept.
	ctx := context.Background()
	for _, target := range []compare.Target{mustTarget(t, env, "nextjs"), mustTarget(t, env, "start"), mustTarget(t, env, "reactrouter")} {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		storeNoCache(t, target.Name, server.Base, storeWriteOf(target.Name))
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
		case "start", "reactrouter":
			for _, h := range []string{"Cache-Control", "ETag", "Last-Modified", "Expires", "Age"} {
				if v := header.Get(h); v != "" {
					t.Errorf("%s answers a page with %s: %s", target.Name, h, v)
				}
			}
		}
	}
}

// scan fails when a source of the app, or the file that lists its packages, holds text.
func scan(t *testing.T, app, text string) {
	t.Helper()
	for _, root := range []string{filepath.Join(app, "src"), filepath.Join(app, "app"), filepath.Join(app, "shop")} {
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
	for _, file := range []string{"package.json", "next.config.ts", "vite.config.ts", "react-router.config.ts"} {
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

// freeAddr returns a loopback address with a port no one listens on now.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func mustTarget(t *testing.T, env compare.Env, name string) compare.Target {
	t.Helper()
	target, err := env.Target(name)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// TestDomainMatchesGoAfterWrites sends the writes of the first 60 reads of the scenario with writes, and
// then every kind of write of the shop's admin (place, ship, note, change the recipient), to the Go
// shop and applies them in the port, and checks that the data of every page, with the pages of the
// orders placed, is still the same. Instants are left out, since the two make them at different times.
func TestDomainMatchesGoAfterWrites(t *testing.T) {
	env := setup(t)
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	buildShop(t, env)
	ctx := context.Background()
	recipients := compare.Writes(compare.ReadWriteScenario, 60)
	if len(recipients) < 5 {
		t.Fatalf("only %d writes", len(recipients))
	}
	writes := compare.RecipientWrites(recipients)
	writes = append(writes, adminWrites()...)
	want, err := env.GoData(ctx, writes...)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.NodeData(ctx, writes...)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || len(want) != len(compare.AllPages())+3 {
		t.Fatalf("%d pages in the shop, %d in the port, want %d", len(want), len(got), len(compare.AllPages())+3)
	}
	for _, d := range compare.DiffData(compare.WithoutInstants(toAny(t, want)), compare.WithoutInstants(toAny(t, got)), 20) {
		t.Error(d)
	}
	last := recipients[len(recipients)-1]
	text, _ := json.Marshal(want)
	if !strings.Contains(string(text), last.Address) {
		t.Errorf("the last write %q is not in the data of the shop", last.Address)
	}
}

// TestWriteChangesThePage writes the address of an order in each server and checks that the page
// of the order shows it, and shows a second write after it and not the first. The servers include
// Tifa with its render memo on, whose cached page must not be served after a write.
func TestWriteChangesThePage(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	page := compare.Pages[2]
	for _, spec := range env.Specs() {
		if strings.HasSuffix(spec.Name, "-jitless") || spec.AcceptEncoding != "" {
			continue
		}
		t.Run(spec.Name, func(t *testing.T) {
			server, err := spec.Start(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			fetch := func() string {
				body, err := compare.Fetch(server.Base, page)
				if err != nil {
					t.Fatal(err)
				}
				return string(body)
			}
			fetch() // into the cache of Tifa
			if strings.Contains(fetch(), "演示地址 0000") {
				t.Fatal("the page shows a written address before any write")
			}
			var written []string
			for _, n := range []int{0, 2} { // reads 0 and 2 write the order SH-1042
				steps := compare.WriteEachScenario.Steps(spec.Write, n)
				if err := compare.Send(ctx, server.Base, steps[0]); err != nil {
					t.Fatal(err)
				}
				written = append(written, compare.Address(n))
				body := fetch()
				if !strings.Contains(body, written[len(written)-1]) {
					t.Errorf("after the write of %q the page does not show it", written[len(written)-1])
				}
				if len(written) == 2 && strings.Contains(body, written[0]) {
					t.Errorf("after the second write the page still shows %q", written[0])
				}
			}
		})
	}
}

// TestFanOutWriteChangesListsStatsAndOtherOrders gives SH-1042 a new customer name in each server
// and checks that the list of its shop, the stats of its shop and the page of another order of the
// old customer (SH-1032, which lists SH-1042 among the orders of that customer) all show something
// new, and that the four apps show the same text after the same write.
func TestFanOutWriteChangesListsStatsAndOtherOrders(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	pages := []compare.Page{
		{Name: "list studio", URL: "/projects/studio"},
		{Name: "stats studio", URL: "/projects/studio/stats"},
		{Name: "order SH-1032", URL: "/projects/studio/orders/SH-1032"},
	}
	after := map[string][]compare.View{}
	for _, spec := range env.Specs() {
		if strings.HasSuffix(spec.Name, "-jitless") || spec.AcceptEncoding != "" || spec.Name == "tifa-nocache" {
			continue
		}
		server, err := spec.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		var before []compare.View
		for _, p := range pages {
			body, err := compare.Fetch(server.Base, p)
			if err != nil {
				t.Fatal(err)
			}
			before = append(before, compare.ParseView(body)) // also puts the page into Tifa's render memo
		}
		if err := compare.Send(ctx, server.Base, compare.WriteStep(spec.Write, compare.WritableOrders[0], 1, true)); err != nil {
			t.Fatal(err)
		}
		for i, p := range pages {
			body, err := compare.Fetch(server.Base, p)
			if err != nil {
				t.Fatal(err)
			}
			v := compare.ParseView(body)
			if compare.Diff(before[i], v) == "" {
				t.Errorf("%s: %s did not change after a fan-out write", spec.Name, p.Name)
			}
			after[spec.Name] = append(after[spec.Name], v)
		}
		// A second write to the same order with the surname of the first (n=7, 1 and 7 share it): the
		// list and the stats must change again, since no write returns them to an earlier state.
		second := map[string]compare.View{}
		for _, p := range pages[:2] {
			second[p.Name] = after[spec.Name][indexOf(pages, p.Name)]
		}
		if err := compare.Send(ctx, server.Base, compare.WriteStep(spec.Write, compare.WritableOrders[0], 7, true)); err != nil {
			t.Fatal(err)
		}
		for _, p := range pages[:2] {
			body, err := compare.Fetch(server.Base, p)
			if err != nil {
				t.Fatal(err)
			}
			if compare.Diff(second[p.Name], compare.ParseView(body)) == "" {
				t.Errorf("%s: %s is the same after a second write with the same surname", spec.Name, p.Name)
			}
		}
		server.Stop()
	}
	for name, views := range after {
		for i, p := range pages {
			if d := compare.Diff(after["tifa"][i], views[i]); d != "" {
				t.Errorf("%s, %s differs from tifa after the write:\n%s", p.Name, name, d)
			}
		}
	}
	if len(after) < 4 {
		t.Errorf("only %d servers were checked", len(after))
	}
}

func indexOf(pages []compare.Page, name string) int {
	for i, p := range pages {
		if p.Name == name {
			return i
		}
	}
	return -1
}
