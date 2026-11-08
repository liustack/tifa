package compare_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// TestFilteredListsMatchGo checks that the port computes the order list for a URL with filters and a
// page as the Go code does: the rows of the page, the counts of the filtered set, the tabs and the
// query it answers, field by field. It also checks the Go data against what each URL must give, so
// a pair of servers that both ignored the query could not pass.
func TestFilteredListsMatchGo(t *testing.T) {
	env := setup(t)
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	buildShop(t, env)
	ctx := context.Background()
	want, err := env.GoFilterData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.NodeFilterData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantAny, gotAny := toAny(t, want), toAny(t, got)
	for _, d := range compare.DiffData(wantAny, gotAny, 20) {
		t.Error(d)
	}
	for _, f := range compare.FilterPages {
		list, _ := want[f.URL][compare.FilterRoute].(map[string]any)
		rows, _ := list["orders"].([]any)
		if f.Rows >= 0 && len(rows) != f.Rows {
			t.Errorf("%s: the shop sends %d rows, want %d", f.URL, len(rows), f.Rows)
		}
		matching, _ := list["matching"].(float64)
		total, _ := list["total"].(float64)
		if total == 0 || int(matching) < len(rows) || (len(rows) > 0 && matching == 0) {
			t.Errorf("%s: %d rows of %v matching, %v in all", f.URL, len(rows), matching, total)
		}
	}
	// Spot checks of the counts: they are over the filtered set, and the totals are over all orders.
	check := func(url, field string, expect float64) {
		t.Helper()
		list, _ := want0(t, wantAny, url).(map[string]any)
		if n, _ := list[field].(float64); n != expect {
			t.Errorf("%s: %s is %v, want %v", url, field, n, expect)
		}
	}
	check(compare.FilterPages[0].URL, "matching", 40)
	check(compare.FilterPages[0].URL, "pages", 2)
	check(compare.FilterPages[0].URL, "total", 42)
	check(compare.FilterPages[1].URL, "matching", 40)
	check(compare.FilterPages[9].URL, "matching", 0)
	check(compare.FilterPages[9].URL, "total", 42)
	// The list sorted by the amount is sorted over both pages: no order of page 2 is bigger than the
	// last of page 1.
	var byAmount [2][]any
	for _, f := range compare.FilterPages {
		switch f.Name {
		case "list studio by amount":
			byAmount[0], _ = want0(t, wantAny, f.URL).(map[string]any)["orders"].([]any)
		case "list studio by amount page 2":
			byAmount[1], _ = want0(t, wantAny, f.URL).(map[string]any)["orders"].([]any)
		}
	}
	cents := func(rows []any, i int) float64 {
		c, _ := rows[i].(map[string]any)["totalCents"].(float64)
		return c
	}
	if len(byAmount[0]) != 25 || len(byAmount[1]) != 17 || cents(byAmount[0], 24) < cents(byAmount[1], 0) || cents(byAmount[0], 0) < cents(byAmount[0], 24) {
		t.Errorf("the list sorted by the amount is not in order across its pages")
	}
}

// want0 is the data of the list page at url.
func want0(t *testing.T, data any, url string) any {
	t.Helper()
	page, _ := data.(map[string]any)[url].(map[string]any)
	list, ok := page[compare.FilterRoute]
	if !ok {
		t.Fatalf("no list data for %s", url)
	}
	return list
}

// TestFilteredListsShowTheSameText builds the shop as a tifa program and as a Next.js, a TanStack
// Start and a React Router app, and checks that an order list with filters and a page in the URL
// shows the same title and text, with as many table rows, links, buttons and inputs, and as many
// rows as the Go code gives it.
func TestFilteredListsShowTheSameText(t *testing.T) {
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
		for _, f := range compare.FilterPages {
			body, err := compare.Fetch(server.Base, f.Page)
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
	for i, f := range compare.FilterPages {
		// The table has a row of headings, then a row for each order. The empty message is outside it.
		if f.Rows >= 0 && len(tifa[i].Text) > 0 && tifa[i].Count["tr"] != f.Rows+1 {
			t.Errorf("%s: tifa shows %d table rows, want the headings and %d orders", f.Name, tifa[i].Count["tr"], f.Rows)
		}
		for _, name := range names[1:] {
			if len(views[name]) <= i {
				continue
			}
			if diff := compare.Diff(tifa[i], views[name][i]); diff != "" {
				t.Errorf("%s, %s differs from tifa:\n%s", f.Name, name, diff)
			}
		}
	}
	// A search shows only the order it finds, the same in all four.
	for _, name := range names {
		for i, f := range compare.FilterPages {
			if f.Name != "list studio order number" || len(views[name]) <= i {
				continue
			}
			text := strings.Join(views[name][i].Text, "\n")
			if !strings.Contains(text, "SH-1005") || strings.Contains(text, "SH-1006") {
				t.Errorf("%s: the search for sh-1005 shows the wrong orders", name)
			}
		}
	}
}
