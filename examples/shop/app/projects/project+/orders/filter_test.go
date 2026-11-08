package orders

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// pick returns the ids of the rows that keep, newest first.
func pick(rows []OrderRow, keep func(OrderRow) bool) []string {
	ids := []string{}
	for _, r := range rows {
		if keep(r) {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func idsOf(rows []OrderRow) []string {
	return pick(rows, func(OrderRow) bool { return true })
}

// A filter covers all the orders of the project, not only the newest page: shipped orders that
// are on page 2 of the list are found from page 1.
func TestListFilterFindsOrdersFromEveryPage(t *testing.T) {
	reset()
	all := allRows("studio")
	shipped := pick(all, func(r OrderRow) bool { return r.Status == StatusShipped })
	if len(shipped) != 40 {
		t.Fatalf("%d shipped orders", len(shipped))
	}
	l := ListOrders("studio", shanghai, ListQuery{Status: StatusShipped})
	if l.Matching != 40 || l.Pages != 2 || l.Filter.Page != 1 || !reflect.DeepEqual(idsOf(l.Orders), shipped[:25]) {
		t.Fatalf("shipped, page 1: matching %d, %d pages, page %d, rows %v", l.Matching, l.Pages, l.Filter.Page, idsOf(l.Orders))
	}
	// SH-1017 and the orders before it are not among the newest 25 of the project.
	l = ListOrders("studio", shanghai, ListQuery{Status: StatusShipped, Page: 2})
	if l.Filter.Page != 2 || !reflect.DeepEqual(idsOf(l.Orders), shipped[25:]) || len(l.Orders) != 15 {
		t.Fatalf("shipped, page 2: page %d, rows %v", l.Filter.Page, idsOf(l.Orders))
	}
	// The pending orders are the newest two, so they fill a short first page.
	l = ListOrders("studio", shanghai, ListQuery{Status: StatusPending})
	if l.Matching != 2 || l.Pages != 1 || !reflect.DeepEqual(idsOf(l.Orders), []string{"SH-1042", "SH-1041"}) {
		t.Fatalf("pending: matching %d, rows %v", l.Matching, idsOf(l.Orders))
	}
}

// The numbers that a filter does not change are over all the orders of the project: the cards, the
// carriers to choose from and the oldest order waiting to ship. The counts of the tabs are over the
// orders the other filters leave.
func TestListFilterCountsTheFilteredSetAndKeepsTheTotals(t *testing.T) {
	reset()
	all := allRows("studio")
	whole := ListOrders("studio", shanghai, ListQuery{})
	if whole.Matching != 42 || whole.Pages != 2 || whole.Statuses != (StatusCounts{All: 42, Pending: 2, Shipped: 40}) {
		t.Fatalf("no filter: matching %d, %d pages, tabs %+v", whole.Matching, whole.Pages, whole.Statuses)
	}
	carrier := ""
	for _, r := range all {
		if r.Carrier != "" {
			carrier = r.Carrier
			break
		}
	}
	by := func(r OrderRow) bool { return r.Carrier == carrier }
	n := len(pick(all, by))
	if n == 0 || n == 40 {
		t.Fatalf("%d orders went with %s", n, carrier)
	}
	l := ListOrders("studio", shanghai, ListQuery{Carrier: carrier})
	if l.Matching != n || l.Statuses != (StatusCounts{All: n, Pending: 0, Shipped: n}) || len(l.Orders) != min(n, ListPageSize) {
		t.Fatalf("carrier %s: matching %d, tabs %+v, %d rows", carrier, l.Matching, l.Statuses, len(l.Orders))
	}
	for _, r := range l.Orders {
		if r.Carrier != carrier {
			t.Errorf("%s went with %s", r.ID, r.Carrier)
		}
	}
	// The status tab that is open does not shrink the tabs: they still show the other statuses.
	l = ListOrders("studio", shanghai, ListQuery{Carrier: carrier, Status: StatusPending})
	if l.Matching != 0 || l.Statuses != (StatusCounts{All: n, Shipped: n}) {
		t.Fatalf("pending with a carrier: matching %d, tabs %+v", l.Matching, l.Statuses)
	}
	// Everything over all the orders stays as it was.
	l.Orders, l.Matching, l.Statuses, l.Filter, l.Pages = nil, 0, StatusCounts{}, ListQuery{}, 0
	whole.Orders, whole.Matching, whole.Statuses, whole.Filter, whole.Pages = nil, 0, StatusCounts{}, ListQuery{}, 0
	if !reflect.DeepEqual(l, whole) {
		t.Errorf("totals changed with the filter:\n%+v\n%+v", l, whole)
	}
	if whole.Total != 42 || whole.Pending != 2 || whole.Shipped != 40 {
		t.Errorf("totals %d, %d pending, %d shipped", whole.Total, whole.Pending, whole.Shipped)
	}
}

func TestListFilterByPeriodAndAmount(t *testing.T) {
	reset()
	all := allRows("studio")
	newest := all[0].PlacedAt
	week := pick(all, func(r OrderRow) bool { return !r.PlacedAt.Before(newest.Add(-7 * 24 * time.Hour)) })
	month := pick(all, func(r OrderRow) bool { return !r.PlacedAt.Before(newest.Add(-30 * 24 * time.Hour)) })
	// The demonstration orders span 30 days, so the longer period keeps all of them.
	if len(week) == 0 || len(week) >= len(month) || len(month) > len(all) {
		t.Fatalf("%d orders in 7 days, %d in 30, %d in all", len(week), len(month), len(all))
	}
	for period, want := range map[string][]string{"7": week, "30": month} {
		l := ListOrders("studio", shanghai, ListQuery{Period: period, Page: 1})
		if l.Matching != len(want) || !reflect.DeepEqual(idsOf(l.Orders), want[:min(len(want), ListPageSize)]) {
			t.Errorf("period %s: matching %d, want %d", period, l.Matching, len(want))
		}
	}
	// The three amounts split the orders: under 200 yuan, 200 to 500 yuan, over 500 yuan.
	ranges := map[string]func(int) bool{
		"low":  func(c int) bool { return c <= 19999 },
		"mid":  func(c int) bool { return c >= 20000 && c <= 50000 },
		"high": func(c int) bool { return c >= 50001 },
	}
	sum := 0
	for amount, in := range ranges {
		want := pick(all, func(r OrderRow) bool { return in(r.TotalCents) })
		l := ListOrders("studio", shanghai, ListQuery{Amount: amount})
		if l.Matching != len(want) || !reflect.DeepEqual(idsOf(l.Orders), want[:min(len(want), ListPageSize)]) {
			t.Errorf("amount %s: matching %d, want %d", amount, l.Matching, len(want))
		}
		sum += l.Matching
	}
	if sum != len(all) {
		t.Errorf("the amounts add up to %d of %d orders", sum, len(all))
	}
	// Filters together keep what every one of them keeps.
	l := ListOrders("studio", shanghai, ListQuery{Period: "30", Amount: "mid", Status: StatusShipped})
	want := pick(all, func(r OrderRow) bool {
		return r.Status == StatusShipped && ranges["mid"](r.TotalCents) && !r.PlacedAt.Before(newest.Add(-30*24*time.Hour))
	})
	if l.Matching != len(want) || !reflect.DeepEqual(idsOf(l.Orders), want[:min(len(want), ListPageSize)]) {
		t.Errorf("period, amount and status: matching %d, want %d", l.Matching, len(want))
	}
}

func TestListSearchMatchesOrderNumbersAndCustomers(t *testing.T) {
	reset()
	all := allRows("studio")
	customer := all[0].Customer
	byCustomer := pick(all, func(r OrderRow) bool { return strings.Contains(r.Customer, customer) })
	if len(byCustomer) < 2 {
		t.Fatalf("%s has %d orders", customer, len(byCustomer))
	}
	l := ListOrders("studio", shanghai, ListQuery{Search: customer})
	if l.Matching != len(byCustomer) || !reflect.DeepEqual(idsOf(l.Orders), byCustomer[:min(len(byCustomer), ListPageSize)]) {
		t.Errorf("search %q: matching %d, rows %v, want %v", customer, l.Matching, idsOf(l.Orders), byCustomer)
	}
	if l.Statuses.All != len(byCustomer) || l.Statuses.Pending+l.Statuses.Shipped != len(byCustomer) {
		t.Errorf("tabs of the search %+v", l.Statuses)
	}
	// An order number, in any case and with spaces around it.
	l = ListOrders("studio", shanghai, ListQuery{Search: "  sh-1030 "})
	if !reflect.DeepEqual(idsOf(l.Orders), []string{"SH-1030"}) || l.Matching != 1 || l.Filter.Search != "sh-1030" {
		t.Errorf("search an order number: %v, matching %d, echoed %q", idsOf(l.Orders), l.Matching, l.Filter.Search)
	}
	// Part of a number finds all the numbers that hold it, from every page.
	l = ListOrders("studio", shanghai, ListQuery{Search: "SH-100"})
	if l.Matching != 9 || l.Orders[0].ID != "SH-1009" || l.Orders[8].ID != "SH-1001" {
		t.Errorf("search SH-100: matching %d, rows %v", l.Matching, idsOf(l.Orders))
	}
}

func TestListFilterThatMatchesNothing(t *testing.T) {
	reset()
	for _, q := range []ListQuery{
		{Search: "nobody by this name"},
		{Carrier: "No Such Carrier"},
		{Status: StatusPending, Carrier: "SF Express"},
		{Search: "nobody", Page: 3},
	} {
		l := ListOrders("studio", shanghai, q)
		if l.Orders == nil || len(l.Orders) != 0 || l.Matching != 0 || l.Pages != 0 || l.Filter.Page != 1 {
			t.Errorf("%+v: %d rows, matching %d, %d pages, page %d", q, len(l.Orders), l.Matching, l.Pages, l.Filter.Page)
		}
		if l.Total != 42 || l.Pending != 2 || l.Shipped != 40 || len(l.Daily.Orders) != 14 || len(l.Carriers) == 0 {
			t.Errorf("%+v: the numbers over all orders are gone: %+v", q, l)
		}
	}
	if l := ListOrders("studio", shanghai, ListQuery{Search: "nobody"}); l.Statuses != (StatusCounts{}) {
		t.Errorf("tabs of a search that finds nothing %+v", l.Statuses)
	}
}

// A page number that is not a page of the result is the nearest one, and a filter that is not one
// of the choices is left out.
func TestListQueryCleansWhatTheURLSays(t *testing.T) {
	reset()
	last := ListOrders("studio", shanghai, ListQuery{Page: 9})
	if last.Filter.Page != 2 || len(last.Orders) != 17 || last.Orders[16].ID != "SH-1001" {
		t.Errorf("page 9 of 2: page %d, %d rows", last.Filter.Page, len(last.Orders))
	}
	if l := ListOrders("studio", shanghai, ListQuery{Page: -4}); l.Filter.Page != 1 || l.Orders[0].ID != "SH-1042" {
		t.Errorf("page -4: page %d", l.Filter.Page)
	}
	l := ListOrders("studio", shanghai, ListQuery{Status: "lost", Period: "365", Amount: "huge", Sort: "customer", Dir: "sideways"})
	if l.Filter != (ListQuery{Sort: SortPlaced, Dir: SortDesc, Page: 1}) || l.Matching != 42 {
		t.Errorf("filters that are not choices: %+v, matching %d", l.Filter, l.Matching)
	}
	l = ListOrders("studio", shanghai, ListQuery{Status: StatusShipped, Carrier: "SF Express", Period: "7", Amount: "low", Search: " 林 "})
	if l.Filter != (ListQuery{Status: StatusShipped, Carrier: "SF Express", Period: "7", Amount: "low", Search: "林", Sort: SortPlaced, Dir: SortDesc, Page: 1}) {
		t.Errorf("filters echoed: %+v", l.Filter)
	}
}

func TestListFilterIsPerProject(t *testing.T) {
	reset()
	l := ListOrders("weekend", shanghai, ListQuery{Status: StatusShipped})
	if l.Matching != 11 || len(l.Orders) != 11 || l.Orders[0].ID != "WK-0200" || l.Total != 12 || l.Pages != 1 {
		t.Errorf("weekend shipped: matching %d, rows %v", l.Matching, idsOf(l.Orders))
	}
	if !slices.IsSorted(l.Carriers) {
		t.Errorf("carriers %v", l.Carriers)
	}
}

// byAmount is the rows ordered by what the sort by amount must give: the amount, and orders of the
// same amount in the order of the unsorted list, newest first, whichever way the amount goes.
func byAmount(rows []OrderRow, desc bool) []OrderRow {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b OrderRow) int {
		if desc {
			return b.TotalCents - a.TotalCents
		}
		return a.TotalCents - b.TotalCents
	})
	return out
}

// A sort covers all the orders of the project and comes before the paging: the biggest order of the
// project is first on page 1, not the biggest of the newest 25.
func TestListSortsEveryOrderBeforeItCutsThePages(t *testing.T) {
	reset()
	all := allRows("studio")
	for _, desc := range []bool{true, false} {
		dir := map[bool]string{true: SortDesc, false: SortAsc}[desc]
		want := idsOf(byAmount(all, desc))
		page1 := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: dir})
		page2 := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: dir, Page: 2})
		got := append(idsOf(page1.Orders), idsOf(page2.Orders)...)
		if !reflect.DeepEqual(got, want) || page1.Filter.Sort != SortAmount || page1.Filter.Dir != dir {
			t.Fatalf("amount %s: %v, want %v", dir, got, want)
		}
	}
	// The biggest order is not among the newest 25, so a sort of the page on screen would miss it.
	biggest := byAmount(all, true)[0].ID
	if slices.Contains(idsOf(all[:ListPageSize]), biggest) {
		t.Fatalf("%s is on the first page of the unsorted list: the test shows nothing", biggest)
	}
	if l := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: SortDesc}); l.Orders[0].ID != biggest {
		t.Errorf("the biggest order is %s, page 1 starts with %s", biggest, l.Orders[0].ID)
	}
}

// Orders of the same amount keep the order of the unsorted list, newest first, both ways.
func TestListSortKeepsTiesInAStableOrder(t *testing.T) {
	reset()
	ties := 0
	for _, dir := range []string{SortAsc, SortDesc} {
		l1 := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: dir})
		l2 := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: dir, Page: 2})
		rows := append(slices.Clone(l1.Orders), l2.Orders...)
		for i := 1; i < len(rows); i++ {
			if rows[i].TotalCents != rows[i-1].TotalCents {
				continue
			}
			ties++
			if rows[i].PlacedAt.After(rows[i-1].PlacedAt) || (rows[i].PlacedAt.Equal(rows[i-1].PlacedAt) && rows[i].ID > rows[i-1].ID) {
				t.Errorf("%s: %s comes after %s with the same amount", dir, rows[i].ID, rows[i-1].ID)
			}
		}
	}
	if ties == 0 {
		t.Fatal("no two orders have the same amount: the test shows nothing")
	}
	// Asking twice gives the same list.
	a := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: SortAsc})
	b := ListOrders("studio", shanghai, ListQuery{Sort: SortAmount, Dir: SortAsc})
	if !reflect.DeepEqual(a, b) {
		t.Error("two answers to the same sort differ")
	}
}

func TestListSortByPlacedTimeAndWithFilters(t *testing.T) {
	reset()
	all := allRows("studio")
	oldest := ListOrders("studio", shanghai, ListQuery{Sort: SortPlaced, Dir: SortAsc})
	want := idsOf(all)
	slices.Reverse(want)
	if !reflect.DeepEqual(idsOf(oldest.Orders), want[:ListPageSize]) || oldest.Orders[0].ID != "SH-1001" {
		t.Errorf("oldest first: %v", idsOf(oldest.Orders))
	}
	// The filters come first, then the sort, then the page.
	shipped := pick(all, func(r OrderRow) bool { return r.Status == StatusShipped })
	var rows []OrderRow
	for _, r := range all {
		if r.Status == StatusShipped {
			rows = append(rows, r)
		}
	}
	want = idsOf(byAmount(rows, false))
	l := ListOrders("studio", shanghai, ListQuery{Status: StatusShipped, Sort: SortAmount, Dir: SortAsc, Page: 2})
	if l.Matching != len(shipped) || !reflect.DeepEqual(idsOf(l.Orders), want[ListPageSize:]) {
		t.Errorf("shipped by amount, page 2: %v, want %v", idsOf(l.Orders), want[ListPageSize:])
	}
	// A sort that is not one of the choices is the default, and the direction alone does not make a
	// sort by the amount.
	def := ListOrders("studio", shanghai, ListQuery{})
	for _, q := range []ListQuery{{Sort: "customer"}, {Dir: "asc"}, {Sort: "", Dir: "up"}} {
		got := ListOrders("studio", shanghai, q)
		if q.Dir == "asc" {
			if got.Filter.Sort != SortPlaced || got.Filter.Dir != SortAsc {
				t.Errorf("%+v: %+v", q, got.Filter)
			}
			continue
		}
		if !reflect.DeepEqual(got.Orders, def.Orders) || got.Filter.Sort != SortPlaced || got.Filter.Dir != SortDesc {
			t.Errorf("%+v: %+v", q, got.Filter)
		}
	}
}

// Only ASCII spaces are trimmed from the search, the same in Go, the TypeScript port and the search box:
// the other characters that some languages call white space stay in the search.
func TestListSearchTrimsAsciiWhitespaceOnly(t *testing.T) {
	reset()
	for search, want := range map[string]int{
		" \t\nsh-1005\r\v\f ": 1,
		"\u0085sh-1005":       0, // NEXT LINE
		"\ufeffsh-1005":       0, // ZERO WIDTH NO-BREAK SPACE
		"\u00a0sh-1005":       0, // NO-BREAK SPACE
	} {
		l := ListOrders("studio", shanghai, ListQuery{Search: search})
		if l.Matching != want {
			t.Errorf("search %q: matching %d, want %d", search, l.Matching, want)
		}
		if want == 1 && l.Filter.Search != "sh-1005" {
			t.Errorf("search %q is echoed as %q", search, l.Filter.Search)
		}
	}
}
