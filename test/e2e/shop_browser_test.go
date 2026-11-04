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
func TestShopPagesInBrowser(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	dir := shopDir(t)
	exe := buildAt(t, dir)
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe)

	name := "e2e-shop-pg"
	work := t.TempDir()
	pw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("playwright-cli", append([]string{"-s=" + name}, args...)...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
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
		{"/store/en-US", "/store/en-US/products"},
		{"/store/en-US/products", "/store/en-US/products/pour-over-kettle"},
		{"/store/en-US/products/pour-over-kettle", "/store/en-US/journal"},
		{"/store/en-US/cart", "/store/en-US/products"},
		{"/store/en-US/checkout", "/store/en-US/journal"},
		{"/store/en-US/journal", "/store/en-US/journal/kettle-photos"},
		{"/store/en-US/journal/kettle-photos", "/store/en-US/products"},
		{"/store/zh-CN/products", "/store/zh-CN"},
	} {
		if tc.page == "/login" {
			pw("open", base+tc.page)
			t.Cleanup(func() { exec.Command("playwright-cli", "-s="+name, "close").Run() })
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
