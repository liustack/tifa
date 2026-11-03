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

// expectDatedInBrowser opens url in a browser with playwright-cli, under the session name, and
// checks that the page carries the age of its data in Server-Timing, with the metrics in want, and
// that every match the router hydrated is dated by it, within the last minute. It closes the
// browser before it returns.
func expectDatedInBrowser(t *testing.T, name, url string, want ...string) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the dates of a hydrated page are not checked")
		return
	}
	dir := t.TempDir()
	pw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("playwright-cli", append([]string{"-s=" + name}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	pw("open", url)
	defer exec.Command("playwright-cli", "-s="+name, "close").Run()
	waitFor(t, "the page to hydrate", func() bool {
		return strings.Contains(pw("eval", `() => document.readyState === "complete" && window.__TSR_ROUTER__?.stores.matches.get().length > 0`), "true")
	})
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
