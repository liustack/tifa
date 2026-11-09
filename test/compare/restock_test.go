package compare

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The stock a run needs follows the cart of each visit as the shop keeps it, from the adds that
// make the cart of a returning visit before the round, and counts what its checkouts buy and the
// orders staff may place, times the margin.
func TestStockForCountsWhatTheCheckoutsOfARunBuy(t *testing.T) {
	store := func(after int, kind, path string, body map[string]any) PlannedWrite {
		return PlannedWrite{After: after, Kind: kind, Store: &StoreRequest{Kind: kind, Path: path, Body: mustJSON(body)}}
	}
	// Given out of order: two more of the kettle in the cart, the cups taken out, 12 of an apron
	// capped at 10 of a line, a checkout, and an add after it, whose line the cart still holds at the end.
	a := VisitWrites{Writes: []PlannedWrite{
		store(3, WriteCheckout, "/store/checkout", map[string]any{"email": "a@b.c"}),
		store(0, WriteAdd, "/store/products/pour-over-kettle", map[string]any{"color": "mist", "quantity": 2}),
		store(1, WriteQuantity, "/store/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 0}),
		store(2, WriteAdd, "/store/products/linen-apron", map[string]any{"color": "oat", "size": "L", "quantity": 12}),
		store(3, WriteCoupon, "/store/cart", map[string]any{"code": "SHANHAI10"}),
		store(4, WriteAdd, "/store/products/oak-tray", map[string]any{"color": "oak", "quantity": 3}),
	}}
	// The demonstration cart as it is, and a ship of the staff, whose order the setup places with the
	// items AdminBody gives its create.
	ship := PlannedWrite{After: 1, Seq: 1, Kind: WriteShip, Project: "studio"}
	b := VisitWrites{Writes: []PlannedWrite{store(2, WriteCheckout, "/zh-CN/store/checkout", map[string]any{"email": "a@b.c"}), ship}}
	// A returning visit buys the cart its setup made, and a note places no order.
	c := VisitWrites{
		Setup:  []StoreRequest{{Kind: WriteAdd, Path: "/store/products/oak-tray", Body: mustJSON(map[string]any{"color": "oak", "quantity": 2})}},
		Writes: []PlannedWrite{store(0, WriteCheckout, "/store/checkout", map[string]any{"email": "a@b.c"}), {After: 0, Seq: 2, Kind: WriteNote, Project: "weekend"}},
	}
	got := StockFor(7, []VisitWrites{a, b, a, c})
	want := map[string]map[string]int{
		"studio": {
			"pour-over-kettle": stockMargin * (3 + 1 + 3 + 1),
			"ceramic-cup":      stockMargin * (2 + 2),
			"linen-apron":      stockMargin * (11 + 1 + 11 + 1),
			"reading-candle":   stockMargin * (1 + 1 + 1 + 1),
			"oak-tray":         stockMargin * (3 + 3 + 2),
		},
	}
	for _, l := range AdminBody(7, ship.Seq, ship.After, WriteCreate, ship.Project).(map[string]any)["items"].([]adminLine) {
		want["studio"][l.Slug] += stockMargin * l.Quantity
	}
	if !maps.EqualFunc(got, want, func(a, b map[string]int) bool { return maps.Equal(a, b) }) {
		t.Errorf("StockFor = %v, want %v", got, want)
	}
	if got := StockFor(7, []VisitWrites{{}, {}}); len(got) != 0 {
		t.Errorf("a run with no order needs %v", got)
	}
}

// Restock lists the products of each shop of the need, then edits each with all its fields as listed
// and the stock the run needs (or the one it has, when that is more). An answer that is not a 200, or
// that says a page was rendered for it, fails it.
func TestRestockEditsEveryProductAndRendersNothing(t *testing.T) {
	list := `{"routes":{"app/projects/project+/products/route":{"status":200,"data":[` +
		`{"slug":"oak-tray","priceCents":58000,"stock":6,"nameEn":"Oak Tray","nameZh":"原木托盘","descriptionEn":"Oak.","descriptionZh":"橡木。"},` +
		`{"slug":"ceramic-cup","priceCents":9800,"stock":40,"nameEn":"Cup","nameZh":"杯","descriptionEn":"Cup.","descriptionZh":"杯子。"}]}}}`
	weekend := `{"routes":{"app/projects/project+/products/route":{"status":200,"data":[` +
		`{"slug":"postcards","priceCents":3200,"stock":120,"nameEn":"Postcards","nameZh":"明信片","descriptionEn":"Cards.","descriptionZh":"卡片。"}]}}}`
	serve := func(render string, status int) (*httptest.Server, func() map[string]map[string]any) {
		var mu sync.Mutex
		edits := map[string]map[string]any{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && r.URL.Path == "/projects/studio/products" && r.URL.Query().Get("_tifa") == "b1.001":
				w.Write([]byte(list))
			case r.Method == "GET" && r.URL.Path == "/projects/weekend/products" && r.URL.Query().Get("_tifa") == "b1.001":
				w.Write([]byte(weekend))
			case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/projects/"):
				if render != "" {
					w.Header().Set("X-Tifa-Render", render)
				}
				var body map[string]any
				data, _ := io.ReadAll(r.Body)
				json.Unmarshal(data, &body)
				mu.Lock()
				edits[strings.Replace(strings.TrimPrefix(r.URL.Path, "/projects/"), "/products/", "/", 1)] = body
				mu.Unlock()
				w.WriteHeader(status)
				w.Write([]byte(`{}`))
			default:
				http.NotFound(w, r)
			}
		}))
		return srv, func() map[string]map[string]any { mu.Lock(); defer mu.Unlock(); return edits }
	}
	spec := Spec{
		EditProduct: func(project, product string) string { return "/projects/" + project + "/products/" + product },
		Products: func(project, buildID string) string {
			return "/projects/" + project + "/products?_tifa=" + buildID + ".001"
		},
	}
	ctx := context.Background()

	srv, edits := serve("none", 200)
	defer srv.Close()
	if err := Restock(ctx, srv.URL, spec, "b1", map[string]map[string]int{"studio": {"oak-tray": 500, "ceramic-cup": 10}, "weekend": {"postcards": 300}}); err != nil {
		t.Fatal(err)
	}
	got := edits()
	want := map[string]map[string]any{
		"studio/oak-tray":    {"priceCents": 58000.0, "stock": 500.0, "nameEn": "Oak Tray", "nameZh": "原木托盘", "descriptionEn": "Oak.", "descriptionZh": "橡木。"},
		"studio/ceramic-cup": {"priceCents": 9800.0, "stock": 40.0, "nameEn": "Cup", "nameZh": "杯", "descriptionEn": "Cup.", "descriptionZh": "杯子。"},
		"weekend/postcards":  {"priceCents": 3200.0, "stock": 300.0, "nameEn": "Postcards", "nameZh": "明信片", "descriptionEn": "Cards.", "descriptionZh": "卡片。"},
	}
	if len(got) != len(want) || !maps.EqualFunc(got, want, func(a, b map[string]any) bool { return maps.Equal(a, b) }) {
		t.Errorf("edits %v, want %v", got, want)
	}

	for _, tc := range []struct {
		render string
		status int
		why    string
	}{{"vm", 200, "rendered a page"}, {"memo", 200, "rendered a page"}, {"", 422, "status 422"}, {"none", 409, "status 409"}} {
		srv, _ := serve(tc.render, tc.status)
		err := Restock(ctx, srv.URL, spec, "b1", map[string]map[string]int{"studio": {}})
		srv.Close()
		if err == nil || !strings.HasPrefix(err.Error(), "restock: ") || !strings.Contains(err.Error(), tc.why) {
			t.Errorf("an edit answered %d with X-Tifa-Render %q: %v, want an error saying %q", tc.status, tc.render, err, tc.why)
		}
	}
}

// The restock follows no redirect: an answer is checked as the server sent it, so a 302 that
// rendered a page fails the restock, though the page it leads to is a list that would pass.
func TestRestockFollowsNoRedirect(t *testing.T) {
	list := `{"routes":{"app/projects/project+/products/route":{"status":200,"data":[{"slug":"oak-tray","priceCents":58000,"stock":6,"nameEn":"Oak Tray","nameZh":"原木托盘","descriptionEn":"Oak.","descriptionZh":"橡木。"}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/studio/products":
			w.Header().Set("X-Tifa-Render", "vm")
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/elsewhere":
			w.Write([]byte(list))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	spec := Spec{
		EditProduct: func(project, product string) string { return "/projects/" + project + "/products/" + product },
		Products:    func(project, buildID string) string { return "/projects/" + project + "/products" },
	}
	err := Restock(context.Background(), srv.URL, spec, "b1", map[string]map[string]int{"studio": {"oak-tray": 9}})
	if err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Errorf("a list that answered a redirect: %v", err)
	}
}
