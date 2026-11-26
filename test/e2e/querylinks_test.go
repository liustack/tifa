package e2e

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The router writes the parameters of a query in the order of their names, as the server writes the
// URL it renders a page at, and the browser puts the address bar in that order before it takes the
// page over. So a link built from the query of the page hydrates with the href the server wrote,
// whatever order the address has.
func TestQueryLinksInBrowser(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	_, exe := build(t, "pages")
	base, _, _ := start(t, exe)
	queryLinksInBrowser(t, "tifa-e2e-ql", base, false)
}

// Under tifa dev React compares the attributes it hydrates and warns when they differ.
func TestQueryLinksInDev(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	d := startDev(t, devApp(t))
	d.waitLog(t, 0, ready)
	queryLinksInBrowser(t, "tifa-e2e-ql-dev", d.base, true)
}

// queryLinksInBrowser opens /catalog and /catalog/plain (the same page without SSR) of base under
// addresses whose query is in another order than the server renders it, or has a number spelled
// another way. Each time the page must hydrate with nothing in the console, ask for no data and add
// no entry to the history, the address bar must hold the parameters in the order of their names,
// and both links must have the href the server wrote, in the page and in what React holds.
// Then it opens the pages with parameters no Load reads, the case the docs describe: the address
// keeps them, an effect reads them, and the links React holds carry them while the HTML the server
// wrote does not. In production React leaves those attributes as the server wrote them and says
// nothing, under tifa dev it warns.
func queryLinksInBrowser(t *testing.T, name, base string, dev bool) {
	t.Helper()
	pw, _ := openBrowser(t, name)
	result := func(js string) string {
		_, out, _ := strings.Cut(pw("eval", js), "### Result\n")
		line, _, _ := strings.Cut(out, "\n")
		line = strings.TrimSpace(line)
		if text, err := strconv.Unquote(line); err == nil {
			return text
		}
		return line
	}
	number := func(what, js string) int {
		t.Helper()
		n, err := strconv.Atoi(result(js))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return n
	}
	// console gives the warnings and errors of the page since the last clear.
	console := func() string { return strings.ToLower(pw("console", "warning")) }
	quiet := func(when string) {
		t.Helper()
		out := console()
		for _, bad := range []string{"hydrat", "did not match", "didn't match", "minified react error", "tifa:"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: the console has %q:\n%s", when, bad, out)
				return
			}
		}
	}
	// requests counts the data requests of the document.
	requests := func() int {
		return number("data requests", `() => performance.getEntriesByType("resource").filter((e) => e.name.includes("_tifa=")).length`)
	}
	entries := func() int { return number("history entries", `() => history.length`) }
	hydrated := func(when string) {
		t.Helper()
		waitFor(t, when, func() bool {
			return result(`() => document.readyState === "complete" && [...document.getElementById("tifa-root").querySelectorAll("*")].every((e) => Object.keys(e).some((key) => key.startsWith("__reactFiber$")))`) == "true"
		})
	}
	address := func() string { return result(`() => location.pathname + location.search + location.hash`) }
	// shows gives what the page shows: the filter of the Load, the tab read in an effect, and the
	// href of each link as the page has it and as React renders it.
	shows := func() string {
		return result(`() => JSON.stringify([document.getElementById("shown")?.textContent, document.getElementById("tab")?.textContent, ...["next", "same"].map((id) => { const a = document.getElementById(id); if (!a) return null; const props = Object.keys(a).find((key) => key.startsWith("__reactProps$")); return [a.getAttribute("href"), a[props].href] })])`)
	}
	// expect waits for the page to show shown and tab, the links to have the hrefs React holds
	// (next, same) and the hrefs of the page (nextHTML, sameHTML).
	expect := func(when, shown, tab, next, nextHTML, same, sameHTML string) {
		t.Helper()
		want := `["` + shown + `","` + tab + `",["` + nextHTML + `","` + next + `"],["` + sameHTML + `","` + same + `"]]`
		var got string
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			if got = shows(); got == want {
				return
			}
		}
		t.Fatalf("%s: the page shows %s, want %s", when, got, want)
	}
	opened := func(open, want string) {
		t.Helper()
		before := entries()
		pw("console", "--clear")
		pw("goto", base+open)
		hydrated(open + " to hydrate")
		if got := address(); got != want {
			t.Errorf("%s: address bar %s, want %s", open, got, want)
		}
		if n := requests(); n != 0 {
			t.Errorf("%s: %d data requests on load", open, n)
		}
		if n := entries(); n != before+1 {
			t.Errorf("%s: %d entries in the history, %d before", open, n, before)
		}
	}

	pw("open", base+"/catalog")
	hydrated("/catalog to hydrate")
	for _, path := range []string{"/catalog", "/catalog/plain"} {
		for _, c := range []struct{ open, address, shown, next, same string }{
			// Another order than the render URL, which sorts the parameters by name.
			{"?q=tolkien&min=5.5", "?min=5.5&q=tolkien", "q=tolkien min=5.5 page=0", "?min=5.5&page=2&q=tolkien", "?min=5.5&q=tolkien"},
			// An empty number and a number written another way: the address is written as the router reads it.
			{"?q=tolkien&page=&min=05.50#top", "?min=5.5&q=tolkien#top", "q=tolkien min=5.5 page=0", "?min=5.5&page=2&q=tolkien", "?min=5.5&q=tolkien"},
		} {
			open := path + c.open
			opened(open, path+c.address)
			expect(open, c.shown, "", path+c.next, path+c.next, path+c.same, path+c.same)
			quiet("opening " + open)
		}
	}

	// Parameters no Load reads stay in the address bar, in the order of their names, and an effect
	// reads them. The server rendered without them, so the links React builds from the query differ
	// from the HTML: React keeps the attributes the server wrote.
	open := "/catalog?utm_source=mail&tab=reviews&q=tolkien"
	opened(open, "/catalog?q=tolkien&tab=reviews&utm_source=mail")
	expect(open, "q=tolkien min=0 page=0", "reviews",
		"/catalog?page=2&q=tolkien&tab=reviews&utm_source=mail", "/catalog?page=2&q=tolkien",
		"/catalog?q=tolkien&tab=reviews&utm_source=mail", "/catalog?q=tolkien")
	if dev {
		if out := console(); !strings.Contains(out, "didn't match the client properties") {
			t.Errorf("%s: React did not warn of the attributes:\n%s", open, out)
		}
	} else {
		quiet("opening " + open)
	}
	// A click goes where React's link goes, with every parameter.
	pw("console", "--clear")
	pw("eval", `() => { window.__sameDocument = true; document.getElementById("next").click(); return true }`)
	waitFor(t, "the next page", func() bool { return strings.Contains(shows(), "page=2") })
	if got := address(); got != "/catalog?page=2&q=tolkien&tab=reviews&utm_source=mail" {
		t.Errorf("after the click: address bar %s", got)
	}
	if result(`() => window.__sameDocument === true`) != "true" {
		t.Error("the click loaded another document")
	}
	quiet("the click")

	// A page without SSR has no HTML to keep: the links are what React builds.
	open = "/catalog/plain?utm_source=mail&tab=reviews&q=tolkien"
	opened(open, "/catalog/plain?q=tolkien&tab=reviews&utm_source=mail")
	expect(open, "q=tolkien min=0 page=0", "reviews",
		"/catalog/plain?page=2&q=tolkien&tab=reviews&utm_source=mail", "/catalog/plain?page=2&q=tolkien&tab=reviews&utm_source=mail",
		"/catalog/plain?q=tolkien&tab=reviews&utm_source=mail", "/catalog/plain?q=tolkien&tab=reviews&utm_source=mail")
	quiet("opening " + open)
}
