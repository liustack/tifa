package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The article pages of test/apps/pages show tifa.HTML values with <HTML>: a note of their Layout
// and the body and notes of an article. A page the server renders carries each value once, in its
// markup, and the hydration data a marker in its place. Navigation data and a page without SSR
// carry the string.
func TestHTMLPages(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "pages")
	base, _, _ := start(t, exe)
	var built struct{ BuildID string }
	readJSON(t, dir+"/.tifa/dist/build.json", &built)

	once := func(target string, status int, render string, carries ...string) string {
		t.Helper()
		got, header, body := get(t, base+target)
		if got != status || header.Get("X-Tifa-Render") != render {
			t.Fatalf("%s: %d, rendered by %q, want %d and %q\n%.500s", target, got, header.Get("X-Tifa-Render"), status, render, body)
		}
		for _, text := range carries {
			if n := strings.Count(body, text); n != 1 {
				t.Errorf("%s: %s is in the page %d times, want once:\n%s", target, text, n, body)
			}
		}
		return body
	}
	marker := `{\"$tifa\":\"html\"}`
	// The VM renders the page, and the render cache answers the next request with the same page.
	first := once("/article/read", 200, "vm", "SHELF_NOTE", "BODY_START", "BODY_END", "NOTE_ONE", "NOTE_TWO")
	if n := strings.Count(first, marker); n != 4 {
		t.Errorf("/article/read: %d markers, want 4:\n%s", n, first)
	}
	if n := strings.Count(first, "data-tifa-html="); n != 4 {
		t.Errorf("/article/read: %d elements of <HTML>, want 4:\n%s", n, first)
	}
	if again := once("/article/read", 200, "cache", "BODY_END"); again != first {
		t.Errorf("the render cache gave another page:\n%s", again)
	}
	// The whole-page cache, made and then hit.
	once("/article/cached", 200, "vm", "SHELF_NOTE", "BODY_END")
	if _, header, body := get(t, base+"/article/cached"); header.Get("Cache-Status") != "tifa; hit" || strings.Count(body, "BODY_END") != 1 || strings.Count(body, marker) != 4 {
		t.Errorf("/article/cached from the whole-page cache: %q\n%s", header.Get("Cache-Status"), body)
	}
	// The page fails on the server and renders again with its error in its place: the Layout still
	// carries its note once, and the data of the page that failed is not in the page.
	broken := once("/article/broken", 500, "vm", "SHELF_NOTE")
	if strings.Contains(broken, "BODY_START") || !strings.Contains(broken, `id="article-error"`) || strings.Count(broken, marker) != 1 {
		t.Errorf("/article/broken:\n%s", broken)
	}
	// A Layout above the article fails on the server: nothing renders the article, and the page has
	// no data of it, so no marker of it either.
	shaky := once("/article/shaky", 500, "vm", "SHELF_NOTE")
	if strings.Contains(shaky, "BODY_START") || strings.Contains(shaky, `id="shaky"`) || !strings.Contains(shaky, `id="article-error"`) || strings.Count(shaky, marker) != 1 {
		t.Errorf("/article/shaky:\n%s", shaky)
	}
	// A page without SSR carries the strings, as it renders in the browser.
	plain := once("/article/plain", 200, "none", "SHELF_NOTE", "BODY_END")
	if strings.Contains(plain, "$tifa") || strings.Contains(plain, "data-tifa-html") {
		t.Errorf("/article/plain carries markers:\n%s", plain)
	}
	// Navigation data carries the strings, from the Loads and from the whole-page cache.
	for _, target := range []string{"/article/read", "/article/cached"} {
		_, _, body := get(t, dataURL(base+target, built.BuildID, "111"))
		for _, part := range []string{`"note":"<p class=shelf>SHELF_NOTE<br/>of the shelf</p>"`, `"notes":["<b>NOTE_ONE</b>","<i>NOTE_TWO</i>"]`, "BODY_END"} {
			if !strings.Contains(body, part) {
				t.Errorf("navigation data of %s lacks %s:\n%.500s", target, part, body)
			}
		}
		if strings.Contains(body, "$tifa") {
			t.Errorf("navigation data of %s carries a marker:\n%.500s", target, body)
		}
	}
	htmlInBrowser(t, "tifa-e2e-html", base, true)
	htmlLinksInBrowser(t, "tifa-links", base)
}

// In tifa dev React checks the markup it hydrates against __html, and warns when they differ.
func TestHTMLPagesInDev(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	d := startDev(t, devApp(t))
	htmlInBrowser(t, "tifa-e2e-html-dev", d.base, false)
	htmlLinksInBrowser(t, "tifa-links-dev", d.base)
}

// htmlInBrowser opens the article pages of base in Chromium and checks that each shows its markup
// without a warning of React: hydrated from the page, after a move through the router, back on a
// page the router keeps, and rendered only in the browser. withBroken also opens the pages whose
// components fail on the server.
func htmlInBrowser(t *testing.T, name, base string, withBroken bool) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the article pages are not checked in a browser")
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
	// is reports whether js gives true, read from the result and not from the code it echoes.
	is := func(js string) bool {
		_, result, _ := strings.Cut(pw("eval", js), "### Result\n")
		line, _, _ := strings.Cut(result, "\n")
		return strings.TrimSpace(line) == "true"
	}
	quiet := func(when string) {
		t.Helper()
		out := pw("console", "warning")
		for _, bad := range []string{"hydrat", "did not match", "Minified React error", "tifa:", "patched up"} {
			if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
				t.Errorf("%s: the console has %q:\n%s", when, bad, out)
				return
			}
		}
	}
	shows := func(when, path string) {
		t.Helper()
		waitFor(t, when, func() bool {
			return is(`() => location.pathname === "` + path + `" && (document.getElementById("body")?.innerHTML ?? "").includes("BODY_END") && (document.getElementById("note")?.innerHTML ?? "").includes("SHELF_NOTE")`)
		})
	}
	hydrated := func(when string) {
		t.Helper()
		waitFor(t, when, func() bool {
			// The markup inside an element of <HTML> is no element of React.
			return is(`() => document.readyState === "complete" && [...document.getElementById("tifa-root").querySelectorAll("*")].filter((e) => !e.parentElement.closest("[data-tifa-html]")).every((e) => Object.keys(e).some((key) => key.startsWith("__reactFiber$")))`)
		})
	}

	pw("open", base+"/article/read")
	defer exec.Command("playwright-cli", "-s="+name, "close").Run()
	hydrated("/article/read to hydrate")
	shows("the article", "/article/read")
	// The body is the markup the server wrote, which React kept.
	if !is(`() => document.getElementById("body").innerHTML.includes('<h2 id="intro">Intro</h2><p class="lead">BODY_START') && document.getElementById("body").innerHTML.includes("<br>next")`) {
		t.Errorf("the body is not the markup of the page: %s", pw("eval", `() => document.getElementById("body").innerHTML.slice(0, 200)`))
	}
	quiet("hydrating /article/read")
	pw("eval", `() => { window.__sameDocument = true; document.getElementById("to-cached").click(); return true }`)
	shows("the router to show /article/cached", "/article/cached")
	pw("eval", `() => { history.back(); return true }`)
	shows("the article again after going back", "/article/read")
	pw("eval", `() => { document.getElementById("to-plain").click(); return true }`)
	shows("the router to show /article/plain", "/article/plain")
	if !is(`() => window.__sameDocument === true`) {
		t.Error("the moves loaded another document")
	}
	if out := pw("network"); !strings.Contains(out, "_tifa=") {
		t.Errorf("the moves fetched no navigation data:\n%s", out)
	}
	quiet("moving between the articles")
	for _, path := range []string{"/article/plain", "/article/cached"} {
		pw("goto", base+path)
		hydrated(path + " to render")
		shows(path, path)
		quiet("opening " + path)
	}
	if withBroken {
		pw("goto", base+"/article/broken")
		hydrated("/article/broken to hydrate")
		waitFor(t, "the error of /article/broken", func() bool {
			return is(`() => document.getElementById("article-error") !== null && document.getElementById("note").innerHTML.includes("SHELF_NOTE")`)
		})
		quiet("opening /article/broken")
		// The Layout above the article fails on the server only. After a move away, back on the page
		// the browser renders the article from data it loads, with no markup of the page to read.
		pw("goto", base+"/article/shaky")
		hydrated("/article/shaky to hydrate")
		waitFor(t, "the error of /article/shaky", func() bool {
			return is(`() => document.getElementById("article-error") !== null && document.getElementById("note").innerHTML.includes("SHELF_NOTE")`)
		})
		pw("eval", `() => { window.__sameDocument = true; document.getElementById("error-to-read").click(); return true }`)
		shows("the router to show /article/read from the error", "/article/read")
		pw("eval", `() => { history.back(); return true }`)
		shows("the article below the Layout that failed on the server", "/article/shaky")
		if !is(`() => window.__sameDocument === true && document.getElementById("shaky") !== null && document.getElementById("article-error") === null`) {
			t.Error("/article/shaky after going back: not the page the router rendered")
		}
		quiet("going back to /article/shaky")
	}
}

// linkChecks is the number of checks in testdata/htmllinks.js.
const linkChecks = 31

// htmlLinksInBrowser runs the checks of testdata/htmllinks.js on /article/links of base in
// Chromium: which links inside <HTML> the router follows in the same document, which the browser
// follows itself, and that a hover, a touch or a focus preloads the page of a link once.
func htmlLinksInBrowser(t *testing.T, name, base string) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the links of the article pages are not checked in a browser")
		return
	}
	code, err := os.ReadFile(filepath.Join("testdata", "htmllinks.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "htmllinks.js"), []byte(strings.ReplaceAll(string(code), "__BASE__", base)), 0o644); err != nil {
		t.Fatal(err)
	}
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
	pw("open", "about:blank")
	defer exec.Command("playwright-cli", "-s="+name, "close").Run()
	// A path relative to the folder: playwright-cli reads files only below it, as it spells it.
	out := pw("run-code", "--filename=htmllinks.js")
	// The result is the string the script returns, written as JSON.
	_, result, _ := strings.Cut(out, "### Result\n")
	result, _, _ = strings.Cut(result, "\n")
	var lines string
	if err := json.Unmarshal([]byte(strings.TrimSpace(result)), &lines); err != nil {
		t.Fatalf("playwright-cli run-code: %v\n%s", err, out)
	}
	passed := 0
	for _, line := range strings.Split(lines, "\n") {
		switch {
		case strings.HasPrefix(line, "PASS "):
			passed++
		case strings.HasPrefix(line, "FAIL "):
			t.Error(line)
		default:
			t.Errorf("not a check: %q", line)
		}
	}
	if !t.Failed() && passed != linkChecks {
		t.Errorf("%d checks passed, want %d:\n%s", passed, linkChecks, lines)
	}
}
