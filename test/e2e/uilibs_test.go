package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var stylesheetHref = regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)"`)

// stylesheetOf fetches the stylesheet a page links, the one file with the CSS of the whole app.
func stylesheetOf(t *testing.T, base, body string) string {
	t.Helper()
	m := stylesheetHref.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the page links no stylesheet")
	}
	status, _, css := get(t, base+m[1])
	if status != 200 {
		t.Fatalf("GET %s: %d", m[1], status)
	}
	return css
}

// test/apps/uilibs has one page for each UI library that Tifa had not been tried with: a button, an
// input, a select, tabs and a popup. The checks are about the first paint: what the server writes
// and the stylesheet it links must carry the styles of the components, as a visitor sees the page
// before the scripts run. The browser side (hydration without console messages, the popup opens,
// the tabs switch) was checked by hand with playwright-cli on each page.
func TestUILibsApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "uilibs")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/uilibs first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)
	base, _, _ := start(t, exe)

	t.Run("daisyui", func(t *testing.T) {
		// daisyUI is CSS classes only, so the styles are in the stylesheet.
		body := renderedPage(t, base+"/daisyui")
		expectInBody(t, "/daisyui", body, `<button class="btn btn-primary">Open dialog</button>`, `class="tabs tabs-border"`, `<dialog class="modal">`)
		css := stylesheetOf(t, base, body)
		for _, rule := range []string{".btn-primary", ".modal-box", ".tab-active", ".select"} {
			if !strings.Contains(css, rule) {
				t.Errorf("the stylesheet lacks %s", rule)
			}
		}
	})

	t.Run("base-ui", func(t *testing.T) {
		// Base UI has no styles of its own. The page's CSS file carries the classes it sets.
		body := renderedPage(t, base+"/base-ui")
		expectInBody(t, "/base-ui", body, `class="bu-button"`, `role="tablist"`, `role="tab"`, `Open dialog`)
		if css := stylesheetOf(t, base, body); !strings.Contains(css, ".bu-button") {
			t.Error("the stylesheet lacks .bu-button")
		}
	})

	t.Run("mantine", func(t *testing.T) {
		// Mantine's ColorSchemeScript puts data-mantine-color-scheme on <html>. Tifa writes the head,
		// so the page gives the attribute through htmlAttributes. Every color variable of the
		// stylesheet depends on it.
		body := renderedPage(t, base+"/mantine")
		expectInBody(t, "/mantine", body, `<html lang="en" data-mantine-color-scheme="light">`, "mantine-Button-root", "mantine-Tabs-tab", "mantine-Select-input")
		css := stylesheetOf(t, base, body)
		for _, rule := range []string{"--mantine-color-blue-filled:", ".m_77c9d27d"} {
			if !strings.Contains(css, rule) {
				t.Errorf("the stylesheet lacks %s", rule)
			}
		}
	})

	t.Run("chakra", func(t *testing.T) {
		// Chakra 3 runs on emotion, which writes the styles of each component next to it.
		body := renderedPage(t, base+"/chakra")
		expectInBody(t, "/chakra", body, "Open dialog", `role="tablist"`)
		if n := strings.Count(body, `<style data-emotion="css`); n < 5 {
			t.Errorf("/chakra has %d emotion style tags, want at least 5", n)
		}
	})

	t.Run("styled", func(t *testing.T) {
		// styled-components keeps the styles of a server render in a sheet, and nothing writes them
		// out unless the app does. A page without the sheet in the tree has class names and no CSS.
		// This page puts a ServerStyleSheet in the tree and writes its styles after the content.
		body := renderedPage(t, base+"/styled")
		expectInBody(t, "/styled", body, "Open dialog", `role="tablist"`)
		if !regexp.MustCompile(`<style[^>]*>[^<]*background:#1d4ed8`).MatchString(body) {
			t.Error("/styled has no style tag with the button rule")
		}
	})

	t.Run("react-aria", func(t *testing.T) {
		body := renderedPage(t, base+"/react-aria")
		expectInBody(t, "/react-aria", body, `class="ra-button"`, `role="tablist"`, "Open dialog", "Banana")
		if css := stylesheetOf(t, base, body); !strings.Contains(css, ".ra-button") {
			t.Error("the stylesheet lacks .ra-button")
		}
	})

	t.Run("antd", func(t *testing.T) {
		t.Skip("any import of antd makes tifa build run for ever: the small-function pass of the SSR bundle (runsAfter in js/vite/plugin.js) is exponential on its code. The page waits in test/apps/uilibs/parked/antd. With the pass patched to visit each function once, antd builds, renders from the VM and hydrates, but its styles (cssinjs, or antd/dist/antd.css with zeroRuntime) are missing from the first paint")
	})
}
