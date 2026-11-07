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

// TestShopOrderListSecondPageInBrowser opens the order list of a project with more than 25 orders,
// goes to page 2 and back, and checks the rows on each page and that the console and the network
// stay clean. Page 2 is not in the document: the browser asks the server for it.
func TestShopOrderListSecondPageInBrowser(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	dir := shopDir(t)
	exe := buildAt(t, dir)
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe)

	pw, _ := openBrowser(t, "e2e-shop-list")
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
	// ids are the order numbers of the rows on screen, and counted is the line under the table.
	ids := `() => [...document.querySelectorAll("tbody tr")].map((r) => r.querySelector('a[href^="/projects/studio/orders/"]')?.textContent).join(",")`
	counted := `() => [...document.querySelectorAll("span")].map((s) => s.textContent).find((s) => /^\d+ orders/.test(s)) ?? ""`
	rows := func() []string { return strings.Split(eval(ids).(string), ",") }
	click := func(name string) {
		t.Helper()
		pw("run-code", fmt.Sprintf(`async (page) => { await page.getByRole('button', { name: %q, exact: true }).click() }`, name))
	}

	pw("open", base+"/projects/studio")
	waitFor(t, "the list to hydrate", func() bool {
		return eval(`() => { const root = document.getElementById("tifa-root"); return !!root && Object.keys(root).some((k) => k.startsWith("__reactContainer$")) }`) == true
	})
	first := rows()
	if len(first) != 25 || first[0] != "SH-1042" || first[24] != "SH-1018" || eval(counted) != "42 orders, 1 to 25" {
		t.Fatalf("page 1: %d rows %v, %v", len(first), first, eval(counted))
	}
	check("page 1")

	click("Page 2")
	waitFor(t, "page 2 to show", func() bool { return eval(counted) == "42 orders, 26 to 42" && rows()[0] == "SH-1017" })
	second := rows()
	if len(second) != 17 || second[16] != "SH-1001" {
		t.Errorf("page 2: %d rows %v", len(second), second)
	}
	check("page 2")

	click("Page 1")
	waitFor(t, "page 1 to show again", func() bool { return rows()[0] == "SH-1042" && eval(counted) == "42 orders, 1 to 25" })
	if again := rows(); strings.Join(again, ",") != strings.Join(first, ",") {
		t.Errorf("page 1 again: %v, want %v", again, first)
	}
	check("page 1 again")
}
