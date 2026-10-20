package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderedPage fetches url and fails unless the VM rendered it with status 200.
func renderedPage(t *testing.T, url string) string {
	t.Helper()
	status, header, body := get(t, url)
	if status != 200 || header.Get("X-Tifa-Render") != "vm" {
		t.Fatalf("GET %s: %d %s, want 200 from the VM:\n%.600s", url, status, header.Get("X-Tifa-Render"), body)
	}
	return body
}

func expectInBody(t *testing.T, url, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %s", url, w)
		}
	}
}

// test/apps/mui is an order admin built with MUI and its emotion styles: an app bar and drawer
// in the root Layout, a dashboard, a paged order table and an order form.
func TestMUIApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "mui")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/mui first")
	}
	exe := buildAt(t, dir)
	typecheck(t, dir)
	base, _, _ := start(t, exe)

	// emotion writes the styles of each component next to it during the render.
	body := renderedPage(t, base+"/")
	expectInBody(t, "/", body, "<title>Dashboard · Northwind admin</title>", "Riley Chen", "Recent orders", "Sales by channel")
	if n := strings.Count(body, `<style data-emotion="css`); n < 50 {
		t.Errorf("/ has %d emotion style tags, want at least 50", n)
	}

	// The Load reads the page from the query: the second page starts at the 26th order.
	body = renderedPage(t, base+"/orders")
	if n := strings.Count(body, `aria-label="select ORD-`); n != 25 {
		t.Errorf("/orders has %d order rows, want 25", n)
	}
	expectInBody(t, "/orders", body, `aria-label="select ORD-1240"`, "1–25 of 240")
	body = renderedPage(t, base+"/orders?page=1")
	expectInBody(t, "/orders?page=1", body, `aria-label="select ORD-1215"`, "26–50 of 240")
	if strings.Contains(body, `aria-label="select ORD-1240"`) {
		t.Error("/orders?page=1 lists the first order")
	}

	body = renderedPage(t, base+"/orders/ORD-1240")
	expectInBody(t, "/orders/ORD-1240", body, "<title>ORD-1240 · Northwind admin</title>", "Ship this order")
	if status, _, _ := get(t, base+"/orders/nope"); status != 404 {
		t.Errorf("GET /orders/nope: %d, want 404", status)
	}
}
