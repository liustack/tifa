package compare_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

func TestStorePagesAreAPlainList(t *testing.T) {
	pages := compare.StorePages()
	seen := map[string]bool{}
	for _, p := range pages {
		if seen[p.URL] {
			t.Errorf("%s twice", p.URL)
		}
		seen[p.URL] = true
		if !strings.HasPrefix(p.URL, "/store/") || !strings.HasPrefix(p.Routes, "app/store/lang+/layout,") {
			t.Errorf("page %+v", p)
		}
	}
	for _, l := range compare.StoreLangs {
		for _, url := range []string{"/store/" + l, "/store/" + l + "/cart", "/store/" + l + "/checkout", "/store/" + l + "/journal", "/store/" + l + "/products?page=2"} {
			if !seen[url] {
				t.Errorf("no page %s", url)
			}
		}
	}
	for _, s := range compare.StoreScenarios() {
		if len(s.Steps) == 0 {
			t.Errorf("scenario %s has no steps", s.Name)
		}
	}
	if len(compare.StoreProducts) != 28 {
		t.Errorf("%d products", len(compare.StoreProducts))
	}
}

// TestStoreDomainMatchesGo runs the same visitors against the Go shop and against the TypeScript
// port in test/compare/domain/store: every storefront page in both languages, and carts that are
// filled, changed, given coupons, checked out and refused. For each step the status, the
// Location and Set-Cookie headers and the JSON data must be equal. When the Go code of the
// storefront changes and the port does not, this fails and names the first steps that differ.
func TestStoreDomainMatchesGo(t *testing.T) {
	env := setup(t)
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	buildShop(t, env)
	ctx := context.Background()
	scenarios := compare.StoreScenarios()

	target, err := env.Target("tifa-nocache")
	if err != nil {
		t.Fatal(err)
	}
	shop, err := target.Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer shop.Stop()
	buildID := shopBuildID(t, env)
	want, err := compare.RunStoreOnShop(ctx, shop.Base, buildID, scenarios)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.RunStoreOnNode(ctx, scenarios)
	if err != nil {
		t.Fatal(err)
	}

	// The scenarios must have reached what they are about: the pages answer 200, the missing ones
	// 404, and the checkouts placed orders.
	pages, placed := 0, 0
	for i, sc := range scenarios {
		for j, st := range sc.Steps {
			r := want[i][j]
			switch {
			case sc.Name == "missing":
				if r.Status != 404 && !strings.Contains(string(mustMarshal(t, r.Body)), `"status":404`) {
					t.Errorf("%s %s did not answer 404 in Go: %d", sc.Name, st, r.Status)
				}
			case strings.HasPrefix(sc.Name, "pages ") && st.Method == "GET" && !strings.Contains(st.URL, "SS-NOSUCHORDER"):
				pages++
				if r.Status != 200 {
					t.Errorf("%s %s answered %d in Go", sc.Name, st, r.Status)
				}
			}
			if st.Method == "POST" && strings.HasSuffix(st.URL, "/checkout") && r.Status == 303 {
				placed++
			}
		}
	}
	if pages < 100 || placed < 6 {
		t.Errorf("the scenarios reached %d pages and %d placed orders", pages, placed)
	}

	compare.NormalizeStore(want)
	compare.NormalizeStore(got)
	diffs := compare.DiffStore(scenarios, want, got, 25)
	for _, d := range diffs {
		t.Error(d)
	}
	if len(diffs) > 0 {
		t.Error("the TypeScript port in test/compare/domain/store no longer matches the Go code of the storefront: port the change")
	}
}

func shopBuildID(t *testing.T, env compare.Env) string {
	t.Helper()
	id, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
