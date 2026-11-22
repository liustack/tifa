package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// No test opens a real browser. A test that reaches the page without useBrowser fails here, and
// a page nobody answers gives up soon.
func init() {
	openBrowser = func(u string) error { panic("a test opened a browser at " + u) }
	secretPageTimeout = 20 * time.Second
}

// useBrowser hands the URL of the page to visit instead of a browser, as a browser would get it:
// in the background, while the command waits.
func useBrowser(t *testing.T, visit func(pageURL string)) {
	t.Helper()
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(u string) error {
		go visit(u)
		return nil
	}
}

// pageSubmit is a browser that opens the page and sends values in its form. It keeps the HTML of
// the page and of the answer for the test.
type pageSubmit struct {
	form, answer chan string
}

func submitValues(t *testing.T, values ...string) *pageSubmit {
	t.Helper()
	return submitPages(t, values)
}

// submitPages is submitValues for a command that opens a page more than once: each page gets the
// next answer, and a page past the answers fails the test.
func submitPages(t *testing.T, answers ...[]string) *pageSubmit {
	t.Helper()
	s := &pageSubmit{form: make(chan string, 8), answer: make(chan string, 8)}
	var mu sync.Mutex
	next := 0
	useBrowser(t, func(u string) {
		mu.Lock()
		i := next
		next++
		mu.Unlock()
		if i >= len(answers) {
			t.Errorf("page %d opened, the test has %d answers", i+1, len(answers))
			return
		}
		page, status := pageGet(u)
		if status != 200 {
			t.Errorf("GET %s: %d", u, status)
		}
		s.form <- page
		body, status := pagePost(u, originOf(u), answers[i]...)
		if status != 200 {
			t.Errorf("POST %s: %d %s", u, status, body)
		}
		s.answer <- body
	})
	return s
}

// forms is the HTML of each page opened so far.
func (s *pageSubmit) forms() string {
	var b strings.Builder
	for {
		select {
		case f := <-s.form:
			b.WriteString(f)
		default:
			return b.String()
		}
	}
}

func originOf(u string) string {
	p, _ := url.Parse(u)
	return p.Scheme + "://" + p.Host
}

func pageGet(u string) (string, int) {
	res, err := http.Get(u)
	if err != nil {
		return err.Error(), 0
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return string(data), res.StatusCode
}

// pagePost sends the form with values in v0, v1 and so on, and Origin when it is not empty.
func pagePost(u, origin string, values ...string) (string, int) {
	return pageRequest(http.MethodPost, u, "", origin, values...)
}

func pageRequest(method, u, host, origin string, values ...string) (string, int) {
	form := url.Values{}
	for i, v := range values {
		form.Set(fieldName(i), v)
	}
	req, err := http.NewRequest(method, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err.Error(), 0
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error(), 0
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return string(data), res.StatusCode
}

var purgeAsk = secretAsk{
	Title:  "A Cloudflare token that clears the cache",
	Why:    "Tifa clears the cache of shop.example.com after each deploy from GitHub Actions.",
	Link:   "https://dash.cloudflare.com/?to=/:account/api-tokens&name=tifa%20cache%20purge",
	Fields: []string{"Token"},
	Skip:   "Leave it empty to skip.",
}

// Without a terminal, the value comes from a page on this machine: the URL is printed, the page
// says what is needed and where to make it, and the value comes back. No output shows the value.
func TestSecretPageRoundTrip(t *testing.T) {
	s := submitValues(t, "tok-s3cret")
	var out bytes.Buffer
	got, err := askSecrets(context.Background(), strings.NewReader(""), &out, purgeAsk)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(got) != 1 || got[0] != "tok-s3cret" {
		t.Errorf("got %q", got)
	}
	form, answer := <-s.form, <-s.answer
	for _, want := range []string{purgeAsk.Title, purgeAsk.Why, `href="https://dash.cloudflare.com/?to=/:account/api-tokens&amp;name=tifa%20cache%20purge"`, `type="password"`, purgeAsk.Skip} {
		if !strings.Contains(form, want) {
			t.Errorf("the page lacks %q:\n%s", want, form)
		}
	}
	if !strings.Contains(answer, "You can close this tab") {
		t.Errorf("answer:\n%s", answer)
	}
	text := out.String()
	if !strings.Contains(text, "http://127.0.0.1:") || !strings.Contains(text, "Waiting") || !strings.Contains(text, "Received") {
		t.Errorf("output:\n%s", text)
	}
	if strings.Contains(text, "tok-s3cret") || strings.Contains(answer, "tok-s3cret") {
		t.Errorf("the value was shown:\n%s\n%s", text, answer)
	}
}

// The page sends no Referer to another site, where the code would leak, but keeps it, and so the
// Origin of its form, on itself: a browser sends Origin: null under no-referrer, which the page
// refuses.
func TestSecretPageReferrer(t *testing.T) {
	p := newSecretPage(purgeAsk, "127.0.0.1:4242", "code")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4242/code", nil))
	if got := w.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("Referrer-Policy %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<meta name="referrer" content="same-origin">`) || !strings.Contains(body, `rel="noopener noreferrer"`) {
		t.Errorf("page:\n%s", body)
	}
}

// Several fields come back in their order.
func TestSecretPageFields(t *testing.T) {
	submitValues(t, "id-1", "ts-good")
	ask := secretAsk{Title: "OAuth client", Fields: []string{"Client ID", "Client secret"}}
	got, err := askSecrets(context.Background(), strings.NewReader(""), io.Discard, ask)
	if err != nil || len(got) != 2 || got[0] != "id-1" || got[1] != "ts-good" {
		t.Errorf("%q %v", got, err)
	}
}

// The page answers only its own URL, at its own Host, a POST only from itself, and once. After
// the value came, the server is gone.
func TestSecretPageRefuses(t *testing.T) {
	type try struct {
		name   string
		status int
		do     func(u string) (string, int)
	}
	useBrowser(t, func(u string) {
		p, _ := url.Parse(u)
		base := p.Scheme + "://" + p.Host
		origin := base
		tries := []try{
			{"no code", 404, func(string) (string, int) { return pageGet(base + "/") }},
			{"wrong code", 404, func(string) (string, int) { return pageGet(base + "/AAAAAAAAAAAAAAAAAAAAAA") }},
			{"code with more", 404, func(u string) (string, int) { return pageGet(u + "/x") }},
			{"wrong code, POST", 404, func(string) (string, int) { return pagePost(base+"/AAAAAAAAAAAAAAAAAAAAAA", origin, "x") }},
			{"wrong Host", 403, func(u string) (string, int) { return pageRequest(http.MethodGet, u, "evil.example:"+p.Port(), "") }},
			{"localhost Host", 403, func(u string) (string, int) { return pageRequest(http.MethodGet, u, "localhost:"+p.Port(), "") }},
			{"wrong Host, POST", 403, func(u string) (string, int) {
				return pageRequest(http.MethodPost, u, "evil.example:"+p.Port(), origin, "x")
			}},
			{"other origin", 403, func(u string) (string, int) { return pagePost(u, "https://evil.example", "x") }},
			{"null origin", 403, func(u string) (string, int) { return pagePost(u, "null", "x") }},
			{"other port", 403, func(u string) (string, int) { return pagePost(u, "http://127.0.0.1:1", "x") }},
			{"PUT", 405, func(u string) (string, int) { return pageRequest(http.MethodPut, u, "", origin, "x") }},
			{"good", 200, func(u string) (string, int) { return pagePost(u, origin, "good") }},
		}
		for _, tr := range tries {
			body, status := tr.do(u)
			if status != tr.status {
				t.Errorf("%s: %d, want %d: %s", tr.name, status, tr.status, body)
			}
		}
	})
	got, err := askSecrets(context.Background(), strings.NewReader(""), io.Discard, purgeAsk)
	if err != nil || len(got) != 1 || got[0] != "good" {
		t.Fatalf("%q %v", got, err)
	}
}

// One value only: a second POST is refused, and once the value is back nobody can reach the page.
func TestSecretPageOnce(t *testing.T) {
	p := newSecretPage(purgeAsk, "127.0.0.1:4242", "code")
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4242/code", strings.NewReader(fieldName(0)+"=v"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://127.0.0.1:4242")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w
	}
	if w := post(); w.Code != 200 {
		t.Fatalf("first: %d", w.Code)
	}
	if w := post(); w.Code != http.StatusGone || strings.Contains(w.Body.String(), "v\"") {
		t.Errorf("second: %d %s", w.Code, w.Body.String())
	}
	if got := <-p.got; len(got) != 1 || got[0] != "v" {
		t.Errorf("got %q", got)
	}
	select {
	case v := <-p.got:
		t.Errorf("a second value came: %q", v)
	default:
	}

	// Over the network: after the value came back, the server is down.
	var page string
	useBrowser(t, func(u string) {
		page = u
		pagePost(u, originOf(u), "v")
	})
	if _, err := askSecrets(context.Background(), strings.NewReader(""), io.Discard, purgeAsk); err != nil {
		t.Fatal(err)
	}
	if body, status := pagePost(page, originOf(page), "again"); status != 0 {
		t.Errorf("a POST after the value came: %d %s", status, body)
	}
}

// Nobody sends the form: after the wait the command fails with an error that says what to do, and
// the page is gone.
func TestSecretPageTimeout(t *testing.T) {
	old := secretPageTimeout
	t.Cleanup(func() { secretPageTimeout = old })
	secretPageTimeout = 100 * time.Millisecond
	pageURL := make(chan string, 1)
	useBrowser(t, func(u string) { pageURL <- u })
	start := time.Now()
	_, err := askSecrets(context.Background(), strings.NewReader(""), io.Discard, purgeAsk)
	if err == nil || !strings.Contains(err.Error(), "no value came from the page") || !strings.Contains(err.Error(), "run the command again") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("waited %v", time.Since(start))
	}
	if _, status := pageGet(<-pageURL); status != 0 {
		t.Errorf("the page still answers: %d", status)
	}
	if got := waitText(10 * time.Minute); got != "10 minutes" {
		t.Errorf("waitText %q", got)
	}
}

// A browser that does not open is no error: the URL is printed for a person or an agent to open.
func TestSecretPageNoBrowser(t *testing.T) {
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(u string) error {
		go pagePost(u, originOf(u), "v")
		return io.ErrUnexpectedEOF
	}
	var out bytes.Buffer
	got, err := askSecrets(context.Background(), strings.NewReader(""), &out, purgeAsk)
	if err != nil || got[0] != "v" || !strings.Contains(out.String(), "browser did not open") {
		t.Errorf("%q %v\n%s", got, err, out.String())
	}
}

// On a terminal, the hidden prompt asks for each field in turn, and no page opens.
func TestSecretPromptOnTerminal(t *testing.T) {
	oldT, oldP := stdinIsTerminal, promptSecret
	t.Cleanup(func() { stdinIsTerminal, promptSecret = oldT, oldP })
	stdinIsTerminal = func(io.Reader) bool { return true }
	var asked []string
	promptSecret = func(_ io.Reader, _ io.Writer, label string) (string, error) {
		asked = append(asked, label)
		return "typed " + label, nil
	}
	got, err := askSecrets(context.Background(), strings.NewReader(""), io.Discard, secretAsk{Fields: []string{"Client ID", "Client secret"}})
	if err != nil || strings.Join(got, ",") != "typed Client ID,typed Client secret" || len(asked) != 2 {
		t.Errorf("%q %v %q", got, err, asked)
	}
}

// Each system opens the URL with its own opener, the URL as one argument and never through a shell.
func TestBrowserCommand(t *testing.T) {
	u := "http://127.0.0.1:5000/abc_-9"
	for goos, want := range map[string]string{
		"darwin":  "open " + u,
		"windows": "rundll32 url.dll,FileProtocolHandler " + u,
		"linux":   "xdg-open " + u,
		"freebsd": "xdg-open " + u,
	} {
		if got := strings.Join(browserCommand(goos, u), " "); got != want {
			t.Errorf("%s: %s, want %s", goos, got, want)
		}
	}
}
