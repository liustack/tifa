package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestShopPagesInBrowser opens every page of the production build of the shop in a real browser.
// Each page must hydrate with nothing in the console above the info level (React reports a
// hydration mismatch as an error) and with no request that failed or answered 4xx or 5xx. Then a
// click on a link of the page must show the next page through the router, without loading the
// document again, with the same two checks.
func TestShopPagesInBrowser(t *testing.T) { shopPagesInBrowser(t, "e2e-shop-pg") }

// The same with server.csp on: a script or a style the policy blocks is an error in the console.
func TestShopPagesInBrowserUnderCSP(t *testing.T) {
	shopPagesInBrowser(t, "e2e-shop-csp", "--server-csp", "on")
}

func shopPagesInBrowser(t *testing.T, session string, args ...string) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	dir := shopDir(t)
	exe := buildAt(t, dir)
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe, args...)

	pw, _ := openBrowser(t, session)
	// eval runs a function in the page and returns its JSON result.
	eval := func(fn string) any {
		t.Helper()
		out := pw("eval", fn)
		_, result, ok := strings.Cut(out, "### Result\n")
		if !ok {
			t.Fatalf("no result in %s", out)
		}
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &v); err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		return v
	}
	// check fails the test when the console has a warning or an error, or a request failed.
	failed := regexp.MustCompile(`=> \[(?:FAILED|[45]\d\d)\]`)
	check := func(where string) {
		t.Helper()
		if out := pw("console", "warning"); !strings.Contains(out, "Total messages: 0 ") {
			t.Errorf("%s: console:\n%s", where, out)
		}
		if out := pw("network"); failed.MatchString(out) {
			t.Errorf("%s: failed requests:\n%s", where, out)
		}
	}
	hydrated := `() => { const root = document.getElementById("tifa-root"); return !!root && Object.keys(root).some((k) => k.startsWith("__reactContainer$")) }`
	here := `() => location.pathname + location.search`

	// Each page, and the link a visitor clicks there to reach the next page.
	for _, tc := range []struct{ page, link string }{
		{"/login", ""}, // the sign-in page has no links
		{"/projects/studio", "/projects/studio/orders/SH-1041"},
		{"/projects/studio/orders/SH-1041", "/projects/studio/stats"},
		{"/projects/studio/stats", "/projects/studio"},
		{"/store", "/store/products"},
		{"/store/products", "/store/products/pour-over-kettle"},
		{"/store/products/pour-over-kettle", "/store/journal"},
		{"/store/cart", "/store/products"},
		{"/store/checkout", "/store/journal"},
		{"/store/journal", "/store/journal/kettle-photos"},
		{"/store/journal/kettle-photos", "/store/products"},
		{"/zh-CN/store/products", "/zh-CN/store"},
	} {
		if tc.page == "/login" {
			pw("open", base+tc.page)
		} else {
			pw("goto", base+tc.page)
		}
		waitFor(t, tc.page+" to hydrate", func() bool { return eval(hydrated) == true })
		check(tc.page)
		if tc.link == "" {
			continue
		}
		// A flag on window survives a navigation of the router and is gone after a load of the
		// document.
		eval(`() => { window.__routerNavigation = true; return true }`)
		pw("run-code", fmt.Sprintf(`async (page) => { await page.locator('a[href=%q]:visible').first().click() }`, tc.link))
		waitFor(t, tc.link+" to show", func() bool { return eval(here) == tc.link })
		if eval(`() => window.__routerNavigation === true`) != true {
			t.Errorf("%s -> %s: the link loaded the document again", tc.page, tc.link)
		}
		check(tc.page + " -> " + tc.link)
	}
}

// orderListBrowser opens the order list of the shop in a browser and gives the helpers the list
// tests use: eval runs a function in the page, rows are the order numbers on screen, counted is the
// line under the table, search is the query string of the page, and check fails the test when the
// console has a warning or a request failed.
type orderListBrowser struct {
	t       *testing.T
	pw      func(args ...string) string
	base    string
	failed  *regexp.Regexp
	session string
}

func newOrderListBrowser(t *testing.T, session string) *orderListBrowser {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	dir := shopDir(t)
	exe := buildAt(t, dir)
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe)
	pw, _ := openBrowser(t, session)
	return &orderListBrowser{t: t, pw: pw, base: base, failed: regexp.MustCompile(`=> \[(?:FAILED|[45]\d\d)\]`)}
}

func (b *orderListBrowser) eval(fn string) any {
	b.t.Helper()
	out := b.pw("eval", fn)
	_, result, ok := strings.Cut(out, "### Result\n")
	if !ok {
		b.t.Fatalf("no result in %s", out)
	}
	var v any
	if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &v); err != nil {
		b.t.Fatalf("result %q: %v", result, err)
	}
	return v
}

func (b *orderListBrowser) check(where string) {
	b.t.Helper()
	if out := b.pw("console", "warning"); !strings.Contains(out, "Total messages: 0 ") {
		b.t.Errorf("%s: console:\n%s", where, out)
	}
	if out := b.pw("network"); b.failed.MatchString(out) {
		b.t.Errorf("%s: failed requests:\n%s", where, out)
	}
}

func (b *orderListBrowser) rows() []string {
	ids := `() => [...document.querySelectorAll("tbody tr")].map((r) => r.querySelector('a[href^="/projects/studio/orders/"]')?.textContent).filter(Boolean).join(",")`
	out := b.eval(ids).(string)
	if out == "" {
		return nil
	}
	return strings.Split(out, ",")
}

func (b *orderListBrowser) counted() string {
	return b.eval(`() => [...document.querySelectorAll("span")].map((s) => s.textContent).find((s) => /^\d+ orders/.test(s)) ?? ""`).(string)
}

// tabs are the three counts on the status tabs, such as "All 42,To ship 2,Shipped 40".
func (b *orderListBrowser) tabs() string {
	return b.eval(`() => [...document.querySelectorAll('[role="tab"]')].map((t) => t.textContent).join(",")`).(string)
}

func (b *orderListBrowser) search() string { return b.eval(`() => location.search`).(string) }

func (b *orderListBrowser) code(js string) { b.t.Helper(); b.pw("run-code", js) }

func (b *orderListBrowser) click(name string) {
	b.t.Helper()
	b.code(fmt.Sprintf(`async (page) => { await page.getByRole('button', { name: %q, exact: true }).click() }`, name))
}

func (b *orderListBrowser) open(path string) {
	b.t.Helper()
	b.pw("open", b.base+path)
	waitFor(b.t, "the list to hydrate", func() bool {
		return b.eval(`() => { const root = document.getElementById("tifa-root"); return !!root && Object.keys(root).some((k) => k.startsWith("__reactContainer$")) }`) == true
	})
}

// TestShopOrderListSecondPageInBrowser opens the order list of a project with more than 25 orders,
// goes to page 2 and back, and checks the rows on each page, the page in the URL, and that the
// console and the network stay clean. Page 2 is not in the document: the browser asks the server
// for the data of the page, and a reload of page 2 renders it as it is.
func TestShopOrderListSecondPageInBrowser(t *testing.T) {
	b := newOrderListBrowser(t, "e2e-shop-list")
	b.open("/projects/studio")
	first := b.rows()
	if len(first) != 25 || first[0] != "SH-1042" || first[24] != "SH-1018" || b.counted() != "42 orders, 1 to 25" || b.search() != "" {
		t.Fatalf("page 1: %d rows %v, %v, %q", len(first), first, b.counted(), b.search())
	}
	b.check("page 1")

	b.click("Page 2")
	waitFor(t, "page 2 to show", func() bool { return b.counted() == "42 orders, 26 to 42" && b.rows()[0] == "SH-1017" })
	second := b.rows()
	if len(second) != 17 || second[16] != "SH-1001" || b.search() != "?page=2" {
		t.Errorf("page 2: %d rows %v, %q", len(second), second, b.search())
	}
	b.check("page 2")

	b.pw("reload")
	waitFor(t, "page 2 after a reload", func() bool { return b.counted() == "42 orders, 26 to 42" })
	if again := b.rows(); strings.Join(again, ",") != strings.Join(second, ",") {
		t.Errorf("page 2 after a reload: %v, want %v", again, second)
	}

	b.click("Page 1")
	waitFor(t, "page 1 to show again", func() bool { return b.rows()[0] == "SH-1042" && b.counted() == "42 orders, 1 to 25" })
	if again := b.rows(); strings.Join(again, ",") != strings.Join(first, ",") || b.search() != "" {
		t.Errorf("page 1 again: %v %q, want %v", again, b.search(), first)
	}
	b.check("page 1 again")
}

// TestShopOrderListFiltersInBrowser filters the order list in a browser. The server filters all the
// orders of the project, so a filter on page 1 finds orders that sit on page 2 of the whole list,
// the counts are those of the filtered set, and the filters are in the URL: a reload and a link
// opened fresh show the same rows.
func TestShopOrderListFiltersInBrowser(t *testing.T) {
	b := newOrderListBrowser(t, "e2e-shop-filters")
	b.open("/projects/studio")
	if b.tabs() != "All 42,To ship 2,Shipped 40" {
		t.Fatalf("tabs %q", b.tabs())
	}

	// Shipped: 40 orders in two pages. The shipped orders of page 2 of the whole list are found on
	// page 1 of the filter, and page 2 of the filter ends with the oldest order.
	b.code(`async (page) => { await page.getByRole('tab', { name: /Shipped/ }).click() }`)
	waitFor(t, "the shipped orders", func() bool { return b.counted() == "40 orders, 1 to 25" })
	shipped := b.rows()
	if len(shipped) != 25 || shipped[0] != "SH-1040" || shipped[24] != "SH-1016" || b.search() != "?status=shipped" || b.tabs() != "All 42,To ship 2,Shipped 40" {
		t.Fatalf("shipped: %d rows %v, %q, %q", len(shipped), shipped, b.search(), b.tabs())
	}
	b.check("shipped")
	b.click("Page 2")
	waitFor(t, "page 2 of the shipped orders", func() bool { return b.counted() == "40 orders, 26 to 40" })
	if rows := b.rows(); len(rows) != 15 || rows[0] != "SH-1015" || rows[14] != "SH-1001" || b.search() != "?status=shipped&page=2" {
		t.Errorf("shipped, page 2: %v, %q", rows, b.search())
	}

	// A search from page 2 goes back to page 1 and looks at all the orders, SH-1005 among them.
	b.code(`async (page) => { await page.getByRole('searchbox', { name: 'Search order numbers or customers' }).fill('sh-1005') }`)
	waitFor(t, "the search", func() bool { return b.counted() == "1 orders, 1 to 1" })
	if rows := b.rows(); len(rows) != 1 || rows[0] != "SH-1005" || b.search() != "?status=shipped&q=sh-1005" || b.tabs() != "All 1,To ship 0,Shipped 1" {
		t.Errorf("search: %v, %q, %q", rows, b.search(), b.tabs())
	}
	b.check("search")

	// The same URL, loaded fresh, shows the same rows and the same counts, and the filters on screen.
	b.open("/projects/studio?status=shipped&q=sh-1005")
	if rows := b.rows(); len(rows) != 1 || rows[0] != "SH-1005" || b.counted() != "1 orders, 1 to 1" || b.tabs() != "All 1,To ship 0,Shipped 1" {
		t.Errorf("fresh load: %v, %q, %q", rows, b.counted(), b.tabs())
	}
	if v := b.eval(`() => document.querySelector('input[type="search"]').value`); v != "sh-1005" {
		t.Errorf("the search box holds %v", v)
	}

	// A filter that matches nothing says so, and a way out clears every filter.
	b.code(`async (page) => { await page.getByRole('searchbox', { name: 'Search order numbers or customers' }).fill('nobody by this name') }`)
	waitFor(t, "an empty result", func() bool { return b.counted() == "0 orders" })
	if b.eval(`() => document.body.textContent.includes("No matching orders")`) != true || b.search() != "?status=shipped&q=nobody+by+this+name" || b.tabs() != "All 0,To ship 0,Shipped 0" {
		t.Errorf("empty: %q, %q", b.search(), b.tabs())
	}
	b.check("empty")
	b.click("View all orders")
	waitFor(t, "every order again", func() bool { return b.counted() == "42 orders, 1 to 25" })
	if b.search() != "" || b.tabs() != "All 42,To ship 2,Shipped 40" || b.eval(`() => document.querySelector('input[type="search"]').value`) != "" {
		t.Errorf("cleared: %q, %q", b.search(), b.tabs())
	}

	// The selects: a carrier, a period and an amount, with the counts of what is left.
	b.open("/projects/studio?carrier=SF+Express&period=30&amount=mid")
	n := b.counted()
	if !regexp.MustCompile(`^\d+ orders`).MatchString(n) || n == "42 orders, 1 to 25" {
		t.Errorf("selects in the URL: %q", n)
	}
	for _, id := range b.rows() {
		if !strings.HasPrefix(id, "SH-") {
			t.Errorf("row %q", id)
		}
	}
	b.check("selects")

	// The select boxes write the URL too, and the tab counts agree with the line under the table.
	b.open("/projects/studio")
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Amount' }).click(); await page.getByRole('option', { name: 'Over ¥500' }).click() }`)
	waitFor(t, "the amount filter", func() bool { return b.search() == "?amount=high" })
	var amountRows int
	if _, err := fmt.Sscanf(b.counted(), "%d orders", &amountRows); err != nil || amountRows == 0 || amountRows >= 42 || !strings.HasPrefix(b.tabs(), fmt.Sprintf("All %d,", amountRows)) {
		t.Errorf("amount high: %q, tabs %q", b.counted(), b.tabs())
	}
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Placed' }).click(); await page.getByRole('option', { name: 'Last 7 days' }).click() }`)
	waitFor(t, "the period filter", func() bool { return b.search() == "?period=7&amount=high" })
	b.check("select boxes")

	// Sorting covers all the orders too: by amount, the biggest order of the shop is first although it
	// sits on page 2 of the list by time. Orders of the same amount stay newest first. A change of sort
	// goes back to page 1, and the third click on the heading is the default sort again.
	b.open("/projects/studio")
	head := func(want []string, search string) {
		t.Helper()
		waitFor(t, "the sort "+search, func() bool { return b.search() == search && len(b.rows()) > 0 && b.rows()[0] == want[0] })
		if rows := b.rows(); len(rows) != 25 || strings.Join(rows[:len(want)], ",") != strings.Join(want, ",") || b.counted() != "42 orders, 1 to 25" {
			t.Errorf("sort %q: %v, %q", search, rows, b.counted())
		}
	}
	b.click("Amount")
	head([]string{"SH-1008", "SH-1032", "SH-1009"}, "?sort=totalCents&dir=desc")
	b.click("Page 2")
	waitFor(t, "page 2 of the sort", func() bool { return b.counted() == "42 orders, 26 to 42" })
	if b.search() != "?sort=totalCents&dir=desc&page=2" {
		t.Errorf("page 2 of the sort: %q", b.search())
	}
	b.click("Page 1")
	b.click("Amount")
	head([]string{"SH-1030", "SH-1024", "SH-1016"}, "?sort=totalCents&dir=asc")
	b.check("sorted")
	b.click("Page 2")
	waitFor(t, "page 2 of the ascending sort", func() bool { return b.counted() == "42 orders, 26 to 42" })
	b.click("Amount")
	head([]string{"SH-1042", "SH-1041", "SH-1040"}, "")
	b.check("default sort again")
}

// holdRequests makes the browser keep the data request of the list whose query holds all of want and
// none of not: the request waits until release is called. The held request and the functions that
// hold it are kept on the page object of the test browser, so the next calls of the test can see them.
func (b *orderListBrowser) holdRequests(want []string, not []string) {
	b.t.Helper()
	b.code(fmt.Sprintf(`async (page) => {
  const want = %s, not = %s
  page.__held = undefined
  page.__match = (url) => url.pathname === '/projects/studio' && url.searchParams.has('_tifa') &&
    want.every((kv) => url.searchParams.get(kv.split('=')[0]) === kv.split('=')[1]) && not.every((k) => !url.searchParams.has(k))
  page.__hold = (route) => { page.__held = route }
  await page.route(page.__match, page.__hold)
}`, jsonList(want), jsonList(not)))
}

func jsonList(items []string) string {
	out, _ := json.Marshal(items)
	return string(out)
}

// waitHeld waits until the browser holds a request, and release lets it go on to the server.
func (b *orderListBrowser) waitHeld() {
	b.t.Helper()
	b.code(`async (page) => { for (let i = 0; i < 100 && !page.__held; i++) await new Promise((r) => setTimeout(r, 50)); if (!page.__held) throw new Error('no request was held') }`)
}

// release stops holding: the handler that was registered is removed, so a later request of the same
// kind goes through, and the request held so far goes on to the server.
func (b *orderListBrowser) release() {
	b.t.Helper()
	b.code(`async (page) => {
  const held = page.__held
  page.__held = undefined
  await page.unroute(page.__match, page.__hold)
  if (held) await held.continue()
}`)
}

// TestShopOrderListChangesWhileLoadingInBrowser makes a second change of the list while the data for
// the first is still on its way. Both changes must be in the URL and on screen, and the search box must
// show the query the URL has. The test holds the data request of the first change in the browser until
// the second is made, so the order is the same on every run.
func TestShopOrderListChangesWhileLoadingInBrowser(t *testing.T) {
	b := newOrderListBrowser(t, "e2e-shop-race")

	// An amount, then a period before the answer for the amount is back.
	b.open("/projects/studio")
	b.holdRequests([]string{"amount=high"}, []string{"period"})
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Amount' }).click(); await page.getByRole('option', { name: 'Over ¥500' }).click() }`)
	b.waitHeld()
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Placed' }).click(); await page.getByRole('option', { name: 'Last 7 days' }).click() }`)
	b.release()
	waitFor(t, "both filters", func() bool { return b.search() == "?period=7&amount=high" })
	waitFor(t, "the answer to both", func() bool {
		return b.eval(`() => document.querySelector('[role="combobox"][aria-label="Amount"]')?.textContent`) == "Over ¥500"
	})
	if v := b.eval(`() => document.querySelector('[role="combobox"][aria-label="Placed"]')?.textContent`); v != "Last 7 days" {
		t.Errorf("the period box shows %v", v)
	}
	b.check("two filters")

	// A search, then a tab before the answer for the search is back. The tab keeps the search.
	b.open("/projects/studio")
	b.holdRequests([]string{"q=sh-1005"}, []string{"status"})
	b.code(`async (page) => { await page.getByRole('searchbox', { name: 'Search order numbers or customers' }).fill('sh-1005') }`)
	b.waitHeld()
	b.code(`async (page) => { await page.getByRole('tab', { name: /Shipped/ }).click() }`)
	b.release()
	waitFor(t, "the search and the tab", func() bool { return b.search() == "?status=shipped&q=sh-1005" })
	waitFor(t, "the answer to both", func() bool { return b.counted() == "1 orders, 1 to 1" })
	if v := b.eval(`() => document.querySelector('input[type="search"]').value`); v != "sh-1005" {
		t.Errorf("the search box shows %v", v)
	}
	if rows := b.rows(); len(rows) != 1 || rows[0] != "SH-1005" {
		t.Errorf("rows %v", rows)
	}
	b.check("search and tab")
}

// back goes one step back in the history of the page.
func (b *orderListBrowser) back() {
	b.t.Helper()
	b.eval(`() => { history.back(); return true }`)
}

// holdAmountAndGoBack opens the list, asks for an amount, holds the data for it, presses the back
// button, and lets the held request go. The page is then back on the list it showed before.
func (b *orderListBrowser) holdAmountAndGoBack() {
	b.t.Helper()
	b.open("/projects/studio")
	b.holdRequests([]string{"amount=high"}, []string{"period"})
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Amount' }).click(); await page.getByRole('option', { name: 'Over ¥500' }).click() }`)
	b.waitHeld()
	waitFor(b.t, "the URL of the held change", func() bool { return b.search() == "?amount=high" })
	b.back()
	waitFor(b.t, "the URL after back", func() bool { return b.search() == "" })
	b.release()
	b.waitSelect("Amount", "Any amount")
}

func (b *orderListBrowser) waitSelect(label, want string) {
	b.t.Helper()
	waitFor(b.t, "the "+label+" box to show "+want, func() bool {
		return b.eval(fmt.Sprintf(`() => document.querySelector('[role="combobox"][aria-label=%q]')?.textContent`, label)) == want
	})
}

// TestShopOrderListBackThenAnotherChangeInBrowser presses the back button while the data for a change
// is still on its way, and then makes another change. The page is back on the query it shows, so the
// change must be built on that query: the amount that the back button cancelled must not come back.
func TestShopOrderListBackThenAnotherChangeInBrowser(t *testing.T) {
	b := newOrderListBrowser(t, "e2e-shop-back1")
	b.holdAmountAndGoBack()
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Placed' }).click(); await page.getByRole('option', { name: 'Last 7 days' }).click() }`)
	waitFor(t, "the period alone", func() bool { return b.search() == "?period=7" })
	b.waitSelect("Amount", "Any amount")
	b.waitSelect("Placed", "Last 7 days")
	b.check("period after back")
}

// TestShopOrderListBackThenTheSameChangeInBrowser asks for the amount again after the back button
// cancelled it. It must be asked for, not dropped for being the same as the cancelled one.
func TestShopOrderListBackThenTheSameChangeInBrowser(t *testing.T) {
	b := newOrderListBrowser(t, "e2e-shop-back2")
	b.holdAmountAndGoBack()
	b.code(`async (page) => { await page.getByRole('combobox', { name: 'Amount' }).click(); await page.getByRole('option', { name: 'Over ¥500' }).click() }`)
	waitFor(t, "the amount again", func() bool { return b.search() == "?amount=high" })
	b.waitSelect("Amount", "Over ¥500")
	waitFor(t, "a filtered list", func() bool { return b.counted() != "42 orders, 1 to 25" })
	b.check("amount again after back")
}
