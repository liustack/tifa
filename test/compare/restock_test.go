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

// The stock a run needs follows the cart of each visit as the shop keeps it, and counts what its
// checkouts buy, the plans taken in order over and over, times the margin.
func TestStockForCountsWhatTheCheckoutsOfARunBuy(t *testing.T) {
	store := func(after, plan int, kind, path string, body map[string]any) PlannedWrite {
		return PlannedWrite{After: after, Plan: plan, Kind: kind, Store: &StoreRequest{Kind: kind, Path: path, Body: mustJSON(body)}}
	}
	plans := []Plan{{ID: 0}, {ID: 1}}
	writes := []PlannedWrite{
		// Plan 0 (given out of order): two more of the kettle in the cart, the cups taken out, 12 of an
		// apron capped at 10 of a line, a checkout, and an add after it that is not bought.
		store(3, 0, WriteCheckout, "/store/checkout", map[string]any{"email": "a@b.c"}),
		store(0, 0, WriteAdd, "/store/products/pour-over-kettle", map[string]any{"color": "mist", "quantity": 2}),
		store(1, 0, WriteQuantity, "/store/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 0}),
		store(2, 0, WriteAdd, "/store/products/linen-apron", map[string]any{"color": "oat", "size": "L", "quantity": 12}),
		store(3, 0, WriteCoupon, "/store/cart", map[string]any{"code": "SHANHAI10"}),
		store(4, 0, WriteAdd, "/store/products/oak-tray", map[string]any{"color": "oak", "quantity": 3}),
		// Plan 1 buys the demonstration cart as it is.
		store(2, 1, WriteCheckout, "/zh-CN/store/checkout", map[string]any{"email": "a@b.c"}),
		// A write of the back office buys nothing.
		{After: 1, Plan: 1, Kind: "status", Project: "studio"},
	}
	// Three visits: plan 0, plan 1, plan 0.
	got := StockFor(plans, writes, 3)
	want := map[string]int{
		"pour-over-kettle": stockMargin * (3 + 3 + 1),
		"ceramic-cup":      stockMargin * 2,
		"linen-apron":      stockMargin * (11 + 11 + 1),
		"reading-candle":   stockMargin * (1 + 1 + 1),
	}
	if !maps.Equal(got, want) {
		t.Errorf("StockFor = %v, want %v", got, want)
	}
	if got := StockFor(plans, nil, 3); len(got) != 0 {
		t.Errorf("a run with no checkout needs %v", got)
	}
}

// Restock lists the products, then edits each with all its fields as listed and the stock the run
// needs (or the one it has, when that is more). An answer that is not a 200, or that says a page was
// rendered for it, fails it.
func TestRestockEditsEveryProductAndRendersNothing(t *testing.T) {
	list := `{"routes":{"app/projects/project+/products/route":{"status":200,"data":[` +
		`{"slug":"oak-tray","priceCents":58000,"stock":6,"nameEn":"Oak Tray","nameZh":"原木托盘","descriptionEn":"Oak.","descriptionZh":"橡木。"},` +
		`{"slug":"ceramic-cup","priceCents":9800,"stock":40,"nameEn":"Cup","nameZh":"杯","descriptionEn":"Cup.","descriptionZh":"杯子。"}]}}}`
	serve := func(render string, status int) (*httptest.Server, func() map[string]map[string]any) {
		var mu sync.Mutex
		edits := map[string]map[string]any{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && r.URL.Path == "/projects/studio/products" && r.URL.Query().Get("_tifa") == "b1.001":
				w.Write([]byte(list))
			case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/projects/studio/products/"):
				if render != "" {
					w.Header().Set("X-Tifa-Render", render)
				}
				var body map[string]any
				data, _ := io.ReadAll(r.Body)
				json.Unmarshal(data, &body)
				mu.Lock()
				edits[strings.TrimPrefix(r.URL.Path, "/projects/studio/products/")] = body
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
	if err := Restock(ctx, http.DefaultClient, srv.URL, spec, "b1", map[string]int{"oak-tray": 500, "ceramic-cup": 10}); err != nil {
		t.Fatal(err)
	}
	got := edits()
	want := map[string]map[string]any{
		"oak-tray":    {"priceCents": 58000.0, "stock": 500.0, "nameEn": "Oak Tray", "nameZh": "原木托盘", "descriptionEn": "Oak.", "descriptionZh": "橡木。"},
		"ceramic-cup": {"priceCents": 9800.0, "stock": 40.0, "nameEn": "Cup", "nameZh": "杯", "descriptionEn": "Cup.", "descriptionZh": "杯子。"},
	}
	if len(got) != len(want) || !maps.EqualFunc(got, want, func(a, b map[string]any) bool { return maps.Equal(a, b) }) {
		t.Errorf("edits %v, want %v", got, want)
	}

	for _, tc := range []struct {
		render string
		status int
		why    string
	}{{"vm", 200, "rendered a page"}, {"memo", 200, "rendered a page"}, {"", 422, "answered 422"}, {"none", 409, "answered 409"}} {
		srv, _ := serve(tc.render, tc.status)
		err := Restock(ctx, http.DefaultClient, srv.URL, spec, "b1", nil)
		srv.Close()
		if err == nil || !strings.HasPrefix(err.Error(), "restock: ") || !strings.Contains(err.Error(), tc.why) {
			t.Errorf("an edit answered %d with X-Tifa-Render %q: %v, want an error saying %q", tc.status, tc.render, err, tc.why)
		}
	}
}
