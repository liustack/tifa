package compare_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	want, _, err := env.GoData(ctx)
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
	for _, p := range compare.CheckedPages() {
		if len(want[p.URL]) == 0 || len(got[p.URL]) != len(want[p.URL]) {
			t.Errorf("%s: Go answered %d routes, Node %d", p.URL, len(want[p.URL]), len(got[p.URL]))
		}
	}
	if len(want) != len(compare.CheckedPages()) || len(got) != len(want) {
		t.Errorf("CheckedPages lists %d pages, Go answered %d, Node %d", len(compare.CheckedPages()), len(want), len(got))
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
		for _, page := range compare.CheckedPages() {
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
		for i, page := range compare.CheckedPages() {
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
		for _, page := range compare.CheckedPages() {
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
	for _, page := range compare.CheckedPages() {
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

// TestDomainMatchesGoAfterWrites sends the writes of the first 60 reads of the scenario with writes,
// and then a staff edit of a product of each shop and every kind of write of the shop's admin (place,
// ship, note, change the recipient), to the Go shop and applies them in the port, and checks that the
// data of every page, with the pages of the orders placed, is still the same. The port makes each
// write at the instant the shop made it, so what follows from the instants (the dispatch hours, the
// heatmap) is the same too. Instants are compared as their Unix milliseconds, and each order placed,
// shipment and note must show at the instant the shop made it.
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
	// The edit comes before the orders, so the order of the tray that adminWrites places takes its new
	// name and price.
	writes = append(writes, compare.DomainWrite{Kind: compare.WriteProduct, Project: "studio", Order: "oak-tray", Body: map[string]any{
		"priceCents": 52000, "stock": 5, "nameEn": " Oak Tray, Large ", "nameZh": "橡木大托盘", "descriptionEn": "White oak, carved from one block.", "descriptionZh": "整块白橡木挖制。"}})
	writes = append(writes, compare.DomainWrite{Kind: compare.WriteProduct, Project: "weekend", Order: "postcards", Body: map[string]any{
		"priceCents": 3800, "stock": 40, "nameEn": " Postcards of Cities ", "nameZh": "城市明信片", "descriptionEn": "Six cards.", "descriptionZh": "六张。"}})
	// An edit changes only the fields it has, and a null one keeps its value: the price alone, the
	// stock alone, and a stock with a null title.
	writes = append(writes,
		compare.DomainWrite{Kind: compare.WriteProduct, Project: "studio", Order: "oak-tray", Body: map[string]any{"priceCents": 53000}},
		compare.DomainWrite{Kind: compare.WriteProduct, Project: "weekend", Order: "postcards", Body: map[string]any{"stock": 41}},
		compare.DomainWrite{Kind: compare.WriteProduct, Project: "studio", Order: "glass-carafe", Body: map[string]any{"stock": 33, "nameZh": nil}})
	writes = append(writes, adminWrites()...)
	want, timed, err := env.GoData(ctx, writes...)
	if err != nil {
		t.Fatal(err)
	}
	// The port must make each write at the instant the shop made it, not read a clock of its own: its
	// clock runs an hour ahead here, so a write timed by it shows in the dispatch hours of the stats.
	t.Setenv("NODE_OPTIONS", "--import=data:text/javascript,const%20now%3DDate.now%3BDate.now%3D()%3D%3Enow()%2B3600000")
	got, err := env.NodeData(ctx, timed...)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || len(want) != len(compare.CheckedPages())+3 {
		t.Fatalf("%d pages in the shop, %d in the port, want %d", len(want), len(got), len(compare.CheckedPages())+3)
	}
	wantData, gotData := compare.InstantsAsMillis(toAny(t, want)), compare.InstantsAsMillis(toAny(t, got))
	for _, d := range compare.DiffData(wantData, gotData, 20) {
		t.Error(d)
	}
	// Each order placed, shipment and note shows at the instant the shop made it, in both.
	placed := map[string]int{"studio": compare.OrderIDs["studio"][1], "weekend": compare.OrderIDs["weekend"][1]}
	prefix := map[string]string{"studio": "SH-%04d", "weekend": "WK-%04d"}
	for _, w := range timed {
		id := w.Order
		if w.Kind == compare.WriteCreate {
			placed[w.Project]++
			id = fmt.Sprintf(prefix[w.Project], placed[w.Project])
		}
		if w.At == 0 {
			continue
		}
		for side, data := range map[string]any{"the shop": wantData, "the port": gotData} {
			o, _ := dig(data, "/projects/"+w.Project+"/orders/"+id, "app/projects/project+/orders/order+/route", "order").(map[string]any)
			var at []any
			switch w.Kind {
			case compare.WriteCreate:
				at = []any{o["placedAt"]}
			case compare.WriteShip:
				at = []any{dig(o, "shipment", "shippedAt")}
			case compare.WriteNote:
				notes, _ := o["notes"].([]any)
				for _, n := range notes {
					at = append(at, dig(n, "at"))
				}
			}
			if !slices.Contains(at, any(float64(w.At))) {
				t.Errorf("%s: the %s of %s is not at %d, the instant the shop made it: %v", side, w.Kind, id, w.At, at)
			}
		}
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

// dig follows keys down nested maps, and gives nil where one is missing.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, _ := v.(map[string]any)
		v = m[k]
	}
	return v
}

// TestTheNodeAppsCacheNothing guards that the three Node apps keep no page: two identical GETs of the
// pages of the site, the workbench and the storefront in both languages, have nothing that lets a
// cache keep them or ask for them again cheaply (compare.CacheSigns). An upgrade of a framework or a
// change of its config that turns on a cache fails here.
func TestTheNodeAppsCacheNothing(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	pages := slices.Concat(compare.Pages, compare.StorePages())
	for _, name := range []string{"nextjs", "start", "reactrouter"} {
		target := mustTarget(t, env, name)
		server, err := target.Start(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range pages {
			for try := range 2 {
				_, header, err := compare.FetchWithHeader(server.Base, page)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					break
				}
				if signs := compare.CacheSigns(header); len(signs) > 0 {
					t.Errorf("%s: GET %s (%d of 2) has %v", name, page.URL, try+1, signs)
				}
			}
		}
		// A crawler gets a document of its own sort, with the User-Agent of each crawler of the mix, and
		// nothing in it may be kept by a cache either (an old URL, a 404, too).
		_, old := compare.CrawlURLs()
		for _, agent := range compare.CrawlerAgents {
			for _, u := range []string{compare.StoreBase("en-US") + "/products/" + compare.StoreProducts()[0], old[0]} {
				for try := range 2 {
					req, _ := http.NewRequest("GET", server.Base+u, nil)
					req.Header.Set("User-Agent", agent.UA)
					req.Header.Set("Accept", "text/html")
					res, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Errorf("%s: %v", name, err)
						break
					}
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
					if signs := compare.CacheSigns(res.Header); len(signs) > 0 {
						t.Errorf("%s: GET %s as %s (%d of 2) has %v", name, u, agent.Name, try+1, signs)
					}
				}
			}
		}
		server.Stop()
	}
}

// TestTheWritesShowOnEveryServer makes every kind of write of the site once against Tifa and the
// three apps, as the check before and after a round does: each must show next where the check reads
// it (the cart, the order of a checkout in the back office, an edited product on its storefront page
// and in the product list, and the orders staff place and ship).
func TestTheWritesShowOnEveryServer(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	buildID := shopBuildID(t, env)
	for _, name := range []string{"tifa", "nextjs", "start", "reactrouter"} {
		var spec compare.Spec
		for _, s := range env.Specs() {
			if s.Name == name {
				spec = s
			}
		}
		server, err := spec.Start(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		id := ""
		if spec.Framework == "tifa" {
			id = buildID
		}
		if err := compare.VerifyWrites(context.Background(), spec, server.Base, id); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		server.Stop()
	}
}
