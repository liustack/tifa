package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/ssr"
)

// policyHashes are the hashes a policy lists.
var policyHashes = regexp.MustCompile(`'sha256-[A-Za-z0-9+/=]+'`)

// expectPolicyOfPage checks that the page at url carries a Content-Security-Policy that lists the
// hash of each inline script of the page and no other hash, and that its 304 carries no policy. A
// page without inline scripts, such as one with SSR = false, gets the policy as it is.
func expectPolicyOfPage(t *testing.T, url string, status int) {
	t.Helper()
	got, header, body := get(t, url)
	if got != status {
		t.Errorf("%s: %d, want %d\n%.500s", url, got, status, body)
		return
	}
	policy := header.Get("Content-Security-Policy")
	scripts, _ := ssr.InlineHashes(body)
	listed := policyHashes.FindAllString(policy, -1)
	if !strings.HasPrefix(policy, "script-src 'self'") {
		t.Errorf("%s: policy %q for the inline scripts %q", url, policy, scripts)
	}
	for _, hash := range scripts {
		if !slices.Contains(listed, hash) {
			t.Errorf("%s: the policy %q does not allow the inline script %s", url, policy, hash)
		}
	}
	for _, hash := range listed {
		if !slices.Contains(scripts, hash) {
			t.Errorf("%s: the policy allows %s, which is no inline script of the page", url, hash)
		}
	}
	if tag := header.Get("ETag"); tag != "" {
		code, again, _ := get(t, url, "If-None-Match", tag)
		if code == 304 && again.Get("Content-Security-Policy") != "" {
			t.Errorf("%s: the 304 carries the policy %q", url, again.Get("Content-Security-Policy"))
		}
	}
}

// blocked reports whether line, a message of the console, is a warning or an error, where a browser
// reports a script or a style the policy blocked. The 404 of /favicon.ico, which the test apps do
// not have, is no such message.
func blocked(line string) bool {
	return (strings.HasPrefix(line, "[ERROR]") || strings.HasPrefix(line, "[WARNING]")) && !strings.Contains(line, "/favicon.ico")
}

// expectPagesInBrowser opens each path of base in a real browser under the session name. Each page
// must hydrate with no warning or error in the console (see blocked) and with no request that
// failed.
func expectPagesInBrowser(t *testing.T, name, base string, paths ...string) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the pages are not checked in a browser")
		return
	}
	pw, _ := openBrowser(t, name)
	eval := func(fn string) any {
		t.Helper()
		_, result, ok := strings.Cut(pw("eval", fn), "### Result\n")
		if !ok {
			t.Fatalf("no result of %s", fn)
		}
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &v); err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		return v
	}
	failed := regexp.MustCompile(`=> \[(?:FAILED|[45]\d\d)\]`)
	hydrated := `() => { const root = document.getElementById("tifa-root"); return !!root && Object.keys(root).some((k) => k.startsWith("__reactContainer$")) }`
	for i, path := range paths {
		if i == 0 {
			pw("open", base+path)
		} else {
			pw("goto", base+path)
		}
		waitFor(t, path+" to hydrate", func() bool { return eval(hydrated) == true })
		if out := pw("console", "warning"); slices.ContainsFunc(strings.Split(out, "\n"), blocked) {
			t.Errorf("%s: console:\n%s", path, out)
		}
		if out := pw("network"); failed.MatchString(out) {
			t.Errorf("%s: failed requests:\n%s", path, out)
		}
	}
}

// TestPagesUnderCSP starts the pages app with server.csp on. Every page tifa writes, from the VM,
// the render cache and the whole-page cache, a page without SSR, an error page and a 404, lists
// the hash of each inline script it holds and no other, its data and the markup of tifa.HTML values
// included, and each hydrates in a browser with nothing blocked.
func TestPagesUnderCSP(t *testing.T) {
	skipUnlessEnabled(t)
	_, exe := build(t, "pages")
	base, _, _ := start(t, exe, "--server-csp", "on")
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/", 200}, {"/hostile", 200}, {"/hostile", 200}, {"/hostile/gone", 404}, {"/article/read", 200},
		{"/article/cached", 200}, {"/article/cached", 200}, {"/article/plain", 200}, {"/article/broken", 500},
		{"/receipt", 200}, {"/no/such/page", 404},
	} {
		expectPolicyOfPage(t, base+tc.path, tc.status)
	}
	expectPagesInBrowser(t, "tifa-e2e-csp", base, "/", "/hostile", "/article/read", "/article/plain", "/receipt")
}

// The emotion styles of MUI, which the render writes next to each component and the browser adds
// more of as it runs, are not limited by the default policy.
func TestMUIUnderCSP(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "mui")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/mui first")
	}
	exe := buildAt(t, dir)
	base, _, _ := start(t, exe, "--server-csp", "on")
	for _, path := range []string{"/", "/orders", "/orders/ORD-1240"} {
		expectPolicyOfPage(t, base+path, 200)
	}
	expectPagesInBrowser(t, "tifa-e2e-csp-mui", base, "/", "/orders", "/orders/ORD-1240")
}

// The script and style of the session head, which Go writes at the start of every page of an app
// with sessions, are in the policy.
func TestSessionUnderCSP(t *testing.T) {
	skipUnlessEnabled(t)
	_, exe := build(t, "session")
	base, _, _ := start(t, exe, "--server-csp", "on")
	for _, path := range []string{"/", "/login"} {
		expectPolicyOfPage(t, base+path, 200)
	}
	expectPagesInBrowser(t, "tifa-e2e-csp-ses", base, "/", "/login")
}

// tifa dev sends the policy of server.csp too: the React refresh preamble of Vite's dev server is
// in it, and the pages hydrate with Vite's client running.
func TestDevUnderCSP(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	t.Setenv("SERVER_CSP", "on")
	dir := devApp(t)
	d := startDev(t, dir)
	d.waitLog(t, 0, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" })
	for _, path := range []string{"/", "/hostile", "/article/read"} {
		expectPolicyOfPage(t, d.base+path, 200)
	}
	if _, header, body := get(t, d.base+"/"); !strings.Contains(body, "@react-refresh") || !strings.Contains(header.Get("Content-Security-Policy"), "'sha256-") {
		t.Errorf("the dev page has no preamble or no hashes: %s\n%.500s", header.Get("Content-Security-Policy"), body)
	}
	expectPagesInBrowser(t, "tifa-e2e-csp-dev", d.base, "/", "/hostile", "/article/read")
	expectNoErrors(t, dir, "after the pages under CSP")
}
