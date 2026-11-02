package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

const (
	demoEmail    = "owner@shanhai.example"
	demoPassword = "shanhai-demo"
	demoName     = "顾遥"
)

// checkShopSignIn starts the shop as it is deployed, without SHOP_OPEN, and checks that the
// workbench needs the demo account and the storefront does not.
func checkShopSignIn(t *testing.T, exe string) {
	t.Setenv("SHOP_OPEN", "")
	base, _, _ := start(t, exe)
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(method, path, cookie, body string, header ...string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
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
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header, string(data)
	}

	// Without a session the workbench sends the visitor to the sign-in page, which remembers where
	// they were going. The storefront and the sign-in page need nothing.
	for path, next := range map[string]string{
		"/projects/studio":                 "%2Fprojects%2Fstudio",
		"/projects/studio/orders/SH-1042":  "%2Fprojects%2Fstudio%2Forders%2FSH-1042",
		"/projects/weekend/stats?range=7d": "%2Fprojects%2Fweekend%2Fstats%3Frange%3D7d",
	} {
		if status, header, _ := send("GET", path, "", ""); status != 302 || header.Get("Location") != "/login?next="+next {
			t.Errorf("GET %s signed out: %d %s", path, status, header.Get("Location"))
		}
	}
	for _, path := range []string{"/store/zh-CN", "/store/en-US/products", "/login"} {
		if status, _, _ := send("GET", path, "", ""); status != 200 {
			t.Errorf("GET %s signed out: %d", path, status)
		}
	}
	_, _, login := send("GET", "/login?next=%2Fprojects%2Fstudio", "", "")
	if !strings.Contains(login, "演示账号") {
		t.Errorf("sign-in page: %.400s", login)
	}

	// A wrong password is a 401 and signs nobody in.
	status, header, body := send("POST", "/login", "", `{"email":"`+demoEmail+`","password":"nope","next":""}`, "X-Tifa-Call", "1")
	if status != 401 || header.Get("Set-Cookie") != "" || !strings.Contains(body, "邮箱或密码不对") {
		t.Errorf("wrong password: %d %v %s", status, header, body)
	}
	// The address to go back to stays on this site.
	for next, want := range map[string]string{"/projects/weekend": "/projects/weekend", "//evil.example/": "/projects/studio", "https://evil.example": "/projects/studio"} {
		status, header, _ := send("POST", "/login", "", `{"email":"`+demoEmail+`","password":"`+demoPassword+`","next":"`+next+`"}`)
		if status != 303 || header.Get("Location") != want || !strings.HasPrefix(header.Get("Set-Cookie"), "__Host-tifa_session=") {
			t.Errorf("sign in with next %q: %d %s %v", next, status, header.Get("Location"), header.Get("Set-Cookie"))
		}
	}
	// A call from a page gets the redirect in JSON, and a next that a browser would read as another
	// site is not in it.
	for _, next := range []string{`/\n/evil.example/`, `/\t/evil.example/`, `/%0A/evil.example/`, `/\\evil.example/`} {
		status, header, body := send("POST", "/login", "", `{"email":"`+demoEmail+`","password":"`+demoPassword+`","next":"`+next+`"}`, "X-Tifa-Call", "1")
		if status != 200 || !strings.Contains(body, `"redirect":"/projects/studio"`) {
			t.Errorf("call with next %q goes to another site: %d %v %s", next, status, header, body)
		}
	}
	status, header, _ = send("POST", "/login", "", `{"email":"`+demoEmail+`","password":"`+demoPassword+`","next":""}`)
	cookie := strings.SplitN(header.Get("Set-Cookie"), ";", 2)[0]
	if status != 303 || cookie == "" {
		t.Fatalf("sign in: %d %v", status, header)
	}

	// The page of the signed-in visitor says so, and shows the account in the top bar.
	status, _, page := send("GET", "/projects/studio", cookie, "")
	if status != 200 || !strings.Contains(page, `data-session="in"`) || !strings.Contains(rendered(page), demoName) || !strings.Contains(rendered(page), "退出登录") {
		t.Errorf("workbench signed in: %d, lacks the account", status)
	}
	// Signing out ends the session on the server: the old cookie opens nothing.
	status, header, _ = send("POST", "/logout", cookie, "")
	if status != 303 || header.Get("Location") != "/login" {
		t.Errorf("sign out: %d %s", status, header.Get("Location"))
	}
	if status, _, _ := send("GET", "/projects/studio", cookie, ""); status != 302 {
		t.Errorf("workbench with the cookie of a signed out session: %d", status)
	}

	t.Run("browser", func(t *testing.T) { shopSignInInBrowser(t, base) })
}

// shopSignInInBrowser goes to the workbench as a visitor with no session, signs in through the
// form, and signs out from the top bar.
func shopSignInInBrowser(t *testing.T, base string) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	name := "e2e-shop"
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
	state := func() map[string]any {
		t.Helper()
		out := pw("eval", `() => JSON.stringify({
  path: location.pathname + location.search,
  attribute: document.documentElement.getAttribute("data-session"),
  name: document.querySelector('header [data-testid="account-name"]')?.textContent ?? null,
  nameShown: !!document.querySelector('header [data-testid="account-name"]')?.offsetParent,
  form: !!document.getElementById("login-form"),
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

	pw("open", base+"/projects/studio/stats")
	t.Cleanup(func() { exec.Command("playwright-cli", "-s="+name, "close").Run() })
	s := state()
	if s["path"] != "/login?next=%2Fprojects%2Fstudio%2Fstats" || s["form"] != true || s["attribute"] != "out" {
		t.Fatalf("signed out visitor of the workbench: %v", s)
	}
	pw("fill", ref(`textbox "邮箱"`), demoEmail)
	pw("fill", ref(`textbox "密码"`), demoPassword)
	pw("click", ref(`button "登录"`))
	waitFor(t, "the workbench to show the account", func() bool { s = state(); return s["name"] == demoName })
	if s["path"] != "/projects/studio/stats" || s["attribute"] != "in" || s["nameShown"] != true {
		t.Errorf("signed in: %v", s)
	}

	pw("click", ref(`button "退出登录"`))
	waitFor(t, "the sign-in page", func() bool { s = state(); return s["form"] == true })
	if s["path"] != "/login" || s["attribute"] != "out" || s["name"] != nil {
		t.Errorf("signed out: %v", s)
	}
}
