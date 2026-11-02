package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sessionKey = "__Host-tifa_session:v1"

// sessionSend sends a request with a Cookie and the headers given and returns the status, the headers and the body.
func sessionSend(t *testing.T, method, url, cookie, body string, header ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(data)
}

// TestSessionApp builds test/apps/session, a login form with a header that shows who is signed in,
// and checks how the session reaches the browser: in the pages, in the answers to navigation and to
// calls, and from /__tifa/session. With playwright-cli installed it also drives a browser.
func TestSessionApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "session")
	typecheck(t, dir)
	base, _, _ := start(t, exe)

	// A page for a visitor with no session says so, and carries where the browser keeps its copy.
	status, _, body := get(t, base+"/")
	if status != 200 || !strings.HasPrefix(body, `<!DOCTYPE html><html data-session="out" lang="en"><head>`) || !strings.Contains(body, `sessionKey:"`+sessionKey+`"`) || !strings.Contains(body, "session:$R[") {
		t.Errorf("signed out page: %d %.600s", status, body)
	}
	for _, want := range []string{`<script>(function(){var h=document.documentElement;if(h.hasAttribute("data-session"))return;`, `<style>html:not([data-session="in"]) [data-session="in"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}

	// Signing in as a program that is no page (curl): the answer keeps its shape, 204 and the cookie.
	status, header, body := sessionSend(t, "POST", base+"/login", "", `{"name":"Ann"}`)
	cookie := header.Get("Set-Cookie")
	if status != 204 || body != "" || !strings.HasPrefix(cookie, "__Host-tifa_session=") {
		t.Fatalf("curl sign in: %d %v %q", status, header, body)
	}
	cookie = strings.SplitN(cookie, ";", 2)[0]
	// 400 is the app's own answer and has no session in it.
	if status, _, body := sessionSend(t, "POST", base+"/login", "", `{"name":""}`, "X-Tifa-Call", "1"); status != 400 || strings.Contains(body, "session") {
		t.Errorf("failed call: %d %s", status, body)
	}

	// The page for the signed-in visitor says so on <html> and carries the part of the session the
	// browser may keep, not the secret.
	status, _, body = get(t, base+"/", "Cookie", cookie)
	if status != 200 || !strings.HasPrefix(body, `<!DOCTYPE html><html data-session="in" lang="en"><head>`) || !strings.Contains(body, `data:$R[`) || !strings.Contains(body, `name:"Ann"`) || strings.Contains(body, "never sent") {
		t.Errorf("signed in page: %d %.700s", status, body)
	}
	// A page that skips SSR carries the session in its boot data.
	status, _, body = get(t, base+"/boot", "Cookie", cookie)
	if status != 200 || !strings.HasPrefix(body, `<!DOCTYPE html><html data-session="in"><head>`) || !strings.Contains(body, `"session":{"signedIn":true,"data":{"name":"Ann"}}`) || strings.Contains(body, "never sent") {
		t.Errorf("signed in page without SSR: %d %.700s", status, body)
	}
	// The ETag differs for the two visitors, and a conditional request of one is no 304 for the other.
	_, outHeader, _ := get(t, base+"/")
	_, inHeader, _ := get(t, base+"/", "Cookie", cookie)
	if outHeader.Get("ETag") == "" || outHeader.Get("ETag") == inHeader.Get("ETag") {
		t.Errorf("ETags %q %q", outHeader.Get("ETag"), inHeader.Get("ETag"))
	}
	if status, _, _ := sessionSend(t, "GET", base+"/", cookie, "", "If-None-Match", outHeader.Get("ETag")); status != 200 {
		t.Errorf("conditional request of the signed-out page by a signed-in visitor: %d", status)
	}

	// The navigation answer is an envelope, with the session of the visitor.
	var nav struct {
		Routes  map[string]json.RawMessage `json:"routes"`
		Session json.RawMessage            `json:"session"`
	}
	_, _, body = get(t, base+"/", "Cookie", cookie, "X-Tifa-Route", "app/layout", "X-Tifa-Build", buildID(t, dir))
	if err := json.Unmarshal([]byte(body), &nav); err != nil || string(nav.Session) != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("navigation answer: %s (%v)", body, err)
	}

	// /__tifa/session answers the session and is never stored.
	status, header, body = sessionSend(t, "GET", base+"/__tifa/session", cookie, "")
	if status != 200 || body != `{"signedIn":true,"data":{"name":"Ann"}}` || header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("/__tifa/session: %d %v %s", status, header, body)
	}
	if _, _, body := sessionSend(t, "GET", base+"/__tifa/session", "", ""); body != `{"signedIn":false}` {
		t.Errorf("/__tifa/session signed out: %s", body)
	}

	// A call from a page brings the session in the answer.
	status, _, body = sessionSend(t, "POST", base+"/login", "", `{"name":"Bo"}`, "X-Tifa-Call", "1")
	if status != 200 || body != `{"session":{"signedIn":true,"data":{"name":"Bo"}}}` {
		t.Errorf("call: %d %s", status, body)
	}
	status, _, body = sessionSend(t, "POST", base+"/logout", cookie, `{"everywhere":false}`, "X-Tifa-Call", "1")
	if status != 200 || body != `{"session":{"signedIn":false}}` {
		t.Errorf("sign out call: %d %s", status, body)
	}

	t.Run("cold start", func(t *testing.T) { sessionAfterRestart(t, exe) })
	t.Run("browser", func(t *testing.T) { sessionInBrowser(t, base) })
}

func buildID(t *testing.T, dir string) string {
	t.Helper()
	var built struct{ BuildID string }
	readJSON(t, dir+"/.tifa/dist/build.json", &built)
	return built.BuildID
}

// sessionInBrowser signs in and out through the form of the page in a real browser, and checks that
// the header follows without the page loading again, and what the page keeps in localStorage.
func sessionInBrowser(t *testing.T, base string) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	name := "tifa-e2e-session"
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
	pw("open", base+"/login")
	t.Cleanup(func() { exec.Command("playwright-cli", "-s="+name, "close").Run() })
	// eval returns a JSON text of the page's state.
	state := func() map[string]any {
		t.Helper()
		out := pw("eval", `() => JSON.stringify({
  mark: window.__stays ?? null,
  stored: localStorage.getItem("`+sessionKey+`"),
  attribute: document.documentElement.getAttribute("data-session"),
  who: document.getElementById("who")?.textContent ?? null,
  whoShown: !!document.getElementById("who")?.offsetParent,
  signInShown: !!document.getElementById("sign-in")?.offsetParent,
  state: document.getElementById("state")?.textContent ?? null,
  navigations: performance.getEntriesByType("navigation").length,
  checks: performance.getEntriesByType("resource").filter((e) => e.name.endsWith("/__tifa/session")).length,
})`)
		_, result, ok := strings.Cut(out, "### Result\n")
		if !ok {
			t.Fatalf("no result in %s", out)
		}
		var text string
		if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &text); err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	// ref finds the reference of the element a line of the accessibility snapshot describes.
	ref := func(line string) string {
		t.Helper()
		out := pw("snapshot")
		for _, l := range strings.Split(out, "\n") {
			if i := strings.Index(l, "[ref="); strings.Contains(l, line) && i >= 0 {
				return l[i+5 : i+5+strings.Index(l[i+5:], "]")]
			}
		}
		t.Fatalf("no %q in the snapshot:\n%s", line, out)
		return ""
	}

	s := state()
	if s["attribute"] != "out" || s["signInShown"] != true || s["whoShown"] != false || s["stored"] != nil {
		t.Fatalf("signed out: %v", s)
	}
	pw("eval", `() => { window.__stays = "same document"; return 1 }`)
	pw("fill", ref("textbox"), "Ann")
	pw("click", ref(`button "Sign in"`))
	waitFor(t, "the header to show Ann", func() bool { s = state(); return s["who"] == "Ann" })
	if s["mark"] != "same document" || s["navigations"] != float64(1) {
		t.Errorf("the page loaded again: %v", s)
	}
	if s["attribute"] != "in" || s["stored"] != `{"signedIn":true,"data":{"name":"Ann"}}` || s["whoShown"] != true || s["signInShown"] != false || s["state"] != "signed-in=true ready=true" {
		t.Errorf("signed in: %v", s)
	}

	// A whole page load: the server wrote data-session, the page needs no check, and the copy stays.
	pw("goto", base+"/")
	s = state()
	if s["attribute"] != "in" || s["who"] != "Ann" || s["whoShown"] != true || s["stored"] != `{"signedIn":true,"data":{"name":"Ann"}}` || s["checks"] != float64(0) {
		t.Errorf("after a whole page load: %v", s)
	}
	pw("goto", base+"/boot")
	s = state()
	if s["attribute"] != "in" || s["who"] != "Ann" || s["checks"] != float64(0) || !strings.Contains(s["state"].(string), "signed-in=true ready=true") {
		t.Errorf("page without SSR: %v", s)
	}

	pw("eval", `() => { window.__stays = "same document"; return 1 }`)
	pw("click", ref(`button "Sign out"`))
	waitFor(t, "the header to sign out", func() bool { s = state(); return s["whoShown"] == false })
	if s["mark"] != "same document" || s["attribute"] != "out" || s["stored"] != nil || s["signInShown"] != true || s["whoShown"] != false || s["state"] != "signed-in=false ready=true" {
		t.Errorf("signed out again: %v", s)
	}
}

// waitFor polls until ok reports true, for up to ten seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sessionAfterRestart starts the program twice on the same file of sessions. The second process has
// served nothing yet, and its first request, with the cookie of the first, already carries the data
// of the Browser method.
func sessionAfterRestart(t *testing.T, exe string) {
	t.Setenv("SESSION_FILE", filepath.Join(t.TempDir(), "sessions.json"))
	first, cmd, _ := start(t, exe)
	_, header, _ := sessionSend(t, "POST", first+"/login", "", `{"name":"Cy"}`)
	cookie := strings.SplitN(header.Get("Set-Cookie"), ";", 2)[0]
	cmd.Process.Kill()
	cmd.Wait()
	second, _, _ := start(t, exe)
	status, _, body := sessionSend(t, "GET", second+"/__tifa/session", cookie, "")
	if status != 200 || body != `{"signedIn":true,"data":{"name":"Cy"}}` {
		t.Errorf("first request after a restart: %d %s", status, body)
	}
	_, _, page := get(t, second+"/", "Cookie", cookie)
	if !strings.HasPrefix(page, `<!DOCTYPE html><html data-session="in"`) || !strings.Contains(page, `name:"Cy"`) {
		t.Errorf("page after a restart: %.300s", page)
	}
}
