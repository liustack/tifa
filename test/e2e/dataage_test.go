package e2e

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// pageDating is what a browser reads after it hydrated a page: the names of the Server-Timing
// metrics of the navigation, when the data of the page was loaded by the age the server sent (0
// without one), its clock then, and the time each match the router hydrated is dated at.
type pageDating struct {
	Names []string  `json:"names"`
	At    float64   `json:"at"`
	Now   float64   `json:"now"`
	Dates []float64 `json:"dates"`
}

// readDating is the script that reads a pageDating. The router of TanStack puts itself on
// window.__TSR_ROUTER__ in the browser.
const readDating = `() => {
  const nav = performance.getEntriesByType('navigation')[0]
  const timing = nav.serverTiming ?? []
  const age = timing.find((entry) => entry.name === 'tifa-age')
  return JSON.stringify({
    names: timing.map((entry) => entry.name),
    at: age ? performance.timeOrigin + nav.requestStart - age.duration : 0,
    now: Date.now(),
    dates: window.__TSR_ROUTER__.stores.matches.get().map((match) => match.updatedAt),
  })
}`

// expectDatesInBrowser opens url in a browser with playwright-cli, under the session name, and
// checks the Server-Timing of the page for the metrics in want. The page hydrates without an error
// and with its matches undated (0), as the server wrote them. After a preload of the page, when
// dated, the page carries the age of its data and every match the router hydrated is dated by it,
// within the last minute. Otherwise it carries no age and the matches stay undated. It closes the
// browser before it returns.
func expectDatesInBrowser(t *testing.T, name, url string, dated bool, want ...string) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the dates of a hydrated page are not checked")
		return
	}
	pw, _ := openBrowser(t, name)
	pw("open", url)
	waitFor(t, "the page to hydrate", func() bool {
		return strings.Contains(pw("eval", `() => document.readyState === "complete" && window.__TSR_ROUTER__?.stores.matches.get().length > 0`), "true")
	})
	// React hydrates the inside of a Suspense boundary after the rest, and marks each element it
	// hydrated with a property of its own.
	waitFor(t, "React to hydrate the whole page", func() bool {
		return strings.Contains(pw("eval", `() => [...document.getElementById("tifa-root").querySelectorAll("*")].every((e) => Object.keys(e).some((key) => key.startsWith("__reactFiber$")))`), "true")
	})
	// React reports a hydration that does not match the server with an error, minified in a
	// production build (#418 and #425 are those of a text that differs).
	if out := pw("console", "warning"); strings.Contains(strings.ToLower(out), "hydrat") || strings.Contains(out, "did not match") || strings.Contains(out, "Minified React error") {
		t.Errorf("console of %s: %s", url, out)
	}
	if out := pw("eval", `() => window.__TSR_ROUTER__.stores.matches.get().every((match) => match.updatedAt === 0)`); !strings.Contains(out, "true") {
		t.Errorf("%s: the router dated its matches before it loaded anything: %s", url, out)
	}
	pw("eval", `async () => { await window.__TSR_ROUTER__.preloadRoute({ to: location.pathname }); return true }`)
	out := pw("eval", readDating)
	_, result, _ := strings.Cut(out, "### Result\n")
	line, _, _ := strings.Cut(result, "\n")
	var text string
	var got pageDating
	if err := json.Unmarshal([]byte(line), &text); err != nil {
		t.Fatalf("eval: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("eval: %v\n%s", err, text)
	}
	if !dated {
		if slices.Contains(got.Names, "tifa-age") || len(got.Dates) == 0 || slices.ContainsFunc(got.Dates, func(date float64) bool { return date != 0 }) {
			t.Errorf("%s: Server-Timing %q, matches dated %v, want no age and undated matches", url, got.Names, got.Dates)
		}
		return
	}
	for _, metric := range append([]string{"tifa-age"}, want...) {
		if !slices.Contains(got.Names, metric) {
			t.Errorf("Server-Timing of %s has %q, want %q", url, got.Names, metric)
		}
	}
	if got.At <= 0 || got.Now-got.At < 0 || got.Now-got.At > 60000 {
		t.Errorf("%s: data loaded at %f, the browser's clock at %f", url, got.At, got.Now)
	}
	if len(got.Dates) == 0 {
		t.Errorf("%s: the router hydrated no match", url)
	}
	for _, date := range got.Dates {
		if date != got.At {
			t.Errorf("%s: matches dated %v, want %f", url, got.Dates, got.At)
			break
		}
	}
}
