package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

var stylesheet = regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)">`)

// pageStyle fetches the page at url and the stylesheet it links.
func pageStyle(t *testing.T, base, path string) string {
	t.Helper()
	status, _, body := get(t, base+path)
	m := stylesheet.FindStringSubmatch(body)
	if status != 200 || m == nil {
		t.Fatalf("GET %s: %d, want 200 with a stylesheet:\n%.600s", path, status, body)
	}
	status, header, css := get(t, base+m[1])
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "text/css") {
		t.Fatalf("GET %s: %d %s", m[1], status, header.Get("Content-Type"))
	}
	return css
}

func expectCSS(t *testing.T, css string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(css, w) {
			t.Errorf("stylesheet lacks %s:\n%.1500s", w, css)
		}
	}
}

// test/apps/tailwind uses @tailwindcss/vite, imports tailwindcss in its root Layout's stylesheet,
// defines theme colors and imports a component through the @/ alias.
func TestTailwindApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "tailwind")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/tailwind first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)

	base, _, _ := start(t, exe)
	expectBody(t, base+"/", `class="rounded-full bg-sky-100`)
	css := pageStyle(t, base, "/")
	// Utilities from the Layout, the page and the component, and the app's theme.
	expectCSS(t, css, ".bg-paper", "--color-paper", ".text-3xl", ".bg-sky-100")
	for _, unwanted := range []string{"@import", "@theme", ".decoration-wavy"} {
		if strings.Contains(css, unwanted) {
			t.Errorf("stylesheet contains %s", unwanted)
		}
	}
}

// Under tifa dev, Vite's dev server compiles the stylesheet and picks up a class name that a TSX
// edit adds. The server render follows the edit too.
func TestDevTailwind(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	d := startDev(t, copyApp(t, "tailwind"))
	d.waitLog(t, 0, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" })
	// ?direct asks Vite for the stylesheet itself instead of the module that injects it.
	style := d.base + "/__tifa/vite/app/site.css?direct"
	status, header, css := get(t, style)
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "text/css") {
		t.Fatalf("GET %s: %d %v", style, status, header)
	}
	expectCSS(t, css, ".bg-sky-100", "--color-paper")

	mark := d.edit(t, "components/badge.tsx", "rounded-full", "rounded-full decoration-wavy")
	expectSteps(t, "class edit", d.waitLog(t, mark, ready), "bundle")
	expectBody(t, d.base+"/", "rounded-full decoration-wavy")
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, _, css = get(t, style)
		if strings.Contains(css, ".decoration-wavy") || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	expectCSS(t, css, ".decoration-wavy", ".bg-sky-100")
}
