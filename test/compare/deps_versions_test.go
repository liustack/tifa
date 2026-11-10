package compare

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"testing"
)

// staffPages are pages of the back office the check follows with the pages of the storefront.
var staffPages = []string{"/projects/studio", "/projects/studio/stats", "/projects/studio/orders/SH-1042", "/projects/weekend"}

// TestTheDataVersionsFollowTheShop makes each kind of write of the mix on a shop that runs, and
// asks every page of store_deps.json, and a few of the back office, for its data before and after. The
// data version model (versionEffects and the dependencies of a page) says which pages a write changes:
//
//   - a page the model says is unchanged must have the same data, or the model misses a dependency and the
//     expected hit rate would come out too high. That fails at once.
//   - a page the model says is changed and whose data is the same is a dependency the model has and the
//     data does not: those are counted and printed, and more than a tenth of the pages the model says are
//     changed (on the storefront) fails, to have the model made tighter.
//
// The pages of the back office follow a rule that changes all of a shop at once (versions.go), so the
// ones it changes in vain are printed apart and do not fail.
func TestTheDataVersionsFollowTheShop(t *testing.T) {
	env := depsEnv(t)
	d, err := CheckStoreDeps(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	buildID, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Specs(), func(s Spec) bool { return s.Name == "tifa-nocache" })
	server, err := env.Specs()[i].Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	pages := slices.Concat(depsURLs(), staffPages)
	digits := func(u string) string {
		if strings.HasPrefix(u, "/projects/") {
			return ".111"
		}
		return ".11"
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, u := range pages {
			sep := "?"
			if strings.Contains(u, "?") {
				sep = "&"
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", server.Base+u+sep+"_tifa="+buildID+digits(u), nil)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 {
				t.Fatalf("GET %s: %d %v %.200s", u, res.StatusCode, err, body)
			}
			out[u] = string(body)
		}
		return out
	}
	jar, _ := cookiejar.New(nil)
	visitor := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(c *http.Client, method, path string, body []byte, want ...int) []byte {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, server.Base+path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !slices.Contains(want, res.StatusCode) {
			t.Fatalf("%s %s: status %d, want %v: %.200s", method, path, res.StatusCode, want, data)
		}
		return data
	}
	staff := http.DefaultClient

	// The writes, each with the visit the model reads its effects from.
	add := storeAdd(rand.New(rand.NewPCG(1, 2)), "/store", pageProduct("/store/products/walnut-pen"))
	checkout := StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/store/checkout", Body: []byte(storeOrder("en-US")), Status: 303}
	priceEdit := PlannedWrite{After: 0, Seq: 1, Kind: WriteEdit, Project: storefrontShop}
	for seq := 0; ; seq++ {
		priceEdit.Seq = seq
		if _, c := productChange(3, priceEdit, func(string) int { return 0 }); c["priceCents"] != nil {
			break
		}
	}
	stockEdit := PlannedWrite{After: 0, Seq: 1, Kind: WriteEdit, Project: storefrontShop}
	for seq := 0; ; seq++ {
		stockEdit.Seq = seq
		if _, c := productChange(3, stockEdit, func(string) int { return 0 }); c["stock"] != nil {
			break
		}
	}
	create := PlannedWrite{After: 0, Seq: 9, Kind: WriteCreate, Project: storefrontShop}
	type step struct {
		name    string
		effects []effect
		do      func()
	}
	var created string
	steps := []step{
		{"nothing", nil, func() {}},
		{"an add to the cart", versionEffects(3, VisitWrites{Writes: []PlannedWrite{{After: 0, Seq: 5, Kind: WriteAdd, Store: &add}}}), func() {
			send(visitor, add.Method, add.Path, add.Body, 200)
		}},
		{"a checkout", versionEffects(3, VisitWrites{Writes: []PlannedWrite{
			{After: 0, Seq: 5, Kind: WriteAdd, Store: &add}, {After: 2, Seq: 5, Kind: WriteCheckout, Store: &checkout}}}), func() {
			send(visitor, checkout.Method, checkout.Path, checkout.Body, 303)
		}},
		{"an edit of a price", versionEffects(3, VisitWrites{Writes: []PlannedWrite{priceEdit}}), func() {
			slug, change := productChange(3, priceEdit, func(string) int { return 0 })
			send(staff, "PATCH", "/projects/studio/products/"+slug, mustJSON(change), 200)
		}},
		{"an edit of a stock", versionEffects(3, VisitWrites{Writes: []PlannedWrite{stockEdit}}), func() {
			slug, _ := productChange(3, stockEdit, func(string) int { return 0 })
			send(staff, "PATCH", "/projects/studio/products/"+slug, mustJSON(map[string]any{"stock": 5000}), 200)
		}},
		{"an order placed by staff", versionEffects(3, VisitWrites{Writes: []PlannedWrite{create}}), func() {
			var order struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(send(staff, "POST", "/projects/studio/orders", mustJSON(AdminBody(3, create.Seq, create.After, WriteCreate, "studio")), 200), &order); err != nil {
				t.Fatal(err)
			}
			created = order.ID
		}},
	}
	steps = append(steps, step{"a ship", versionEffects(3, VisitWrites{Writes: []PlannedWrite{{After: 0, Seq: 9, Kind: WriteShip, Project: "studio"}}}), func() {
		send(staff, "POST", "/projects/studio/orders/"+created, mustJSON(AdminBody(3, 9, 0, WriteShip, "studio")), 200)
	}})

	before := snapshot()
	var shopMissed, shopVain, shopChanged, staffVain, staffChanged int
	for _, s := range steps {
		s.do()
		after := snapshot()
		changedKeys := map[string]bool{}
		for _, e := range s.effects {
			for _, k := range e.Keys {
				changedKeys[k] = true
			}
		}
		var vain []string
		for _, u := range pages {
			keys, ok := d.pageKeys(u)
			if !ok {
				t.Fatalf("no version keys for %s", u)
			}
			model := slices.ContainsFunc(keys, func(k string) bool { return changedKeys[k] })
			data := before[u] != after[u]
			admin := strings.HasPrefix(u, "/projects/")
			switch {
			case data && !model:
				shopMissed++
				t.Errorf("%s: the data of %s changed and the model says it did not (it changes %v)", s.name, u, slices.Sorted(maps.Keys(changedKeys)))
			case model && admin:
				staffChanged++
				if !data {
					staffVain++
				}
			case model:
				shopChanged++
				if !data {
					shopVain++
					vain = append(vain, u)
				}
			}
		}
		if len(vain) > 0 {
			t.Logf("%s: the model changes %d pages whose data is the same, for example %s", s.name, len(vain), strings.Join(vain[:min(len(vain), 3)], ", "))
		}
		before = after
	}
	t.Logf("the storefront: the model said %d (page, write) pairs changed and the data of %d did not (%.1f%%), the model missed %d; the back office: %d said changed, %d not", shopChanged, shopVain, 100*float64(shopVain)/float64(max(shopChanged, 1)), shopMissed, staffChanged, staffVain)
	if shopChanged == 0 {
		t.Fatal("the model changed no page of the storefront: the writes do nothing")
	}
	if float64(shopVain) > 0.1*float64(shopChanged) {
		t.Errorf("the model says %d of %d pages changed that did not (%.1f%%), more than a tenth: tighten it", shopVain, shopChanged, 100*float64(shopVain)/float64(shopChanged))
	}
}

// depsEnv is the environment of the tests that start the shop, which are off unless TIFA_COMPARE=1, with
// the shop built (TIFA_COMPARE_SKIP_BUILD=1 uses the last build).
func depsEnv(t *testing.T) Env {
	t.Helper()
	if os.Getenv("TIFA_COMPARE") != "1" {
		t.Skip("starts the shop, set TIFA_COMPARE=1 to run it")
	}
	if why := MissingTools(); why != "" {
		t.Skip(why)
	}
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") != "1" {
		var log strings.Builder
		if err := env.BuildShop(context.Background(), &log); err != nil {
			t.Fatalf("%v\n%s", err, &log)
		}
	}
	return env
}
