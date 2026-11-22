package main

// A secret comes from the developer, never through the agent that may run tifa. On a terminal it
// is a hidden prompt. Without one, tifa serves a page on 127.0.0.1 for one value, opens it in the
// browser and prints its URL, and the developer types the value there.
//
// The page is guarded so that only a browser on this machine that was handed the URL can send:
//   - the URL holds a one-time code of 128 random bits, and any other path gets 404. A page of
//     another site cannot post to it without knowing the code.
//   - the Host must be 127.0.0.1:<port>, so a name rebound to 127.0.0.1 reaches nothing.
//   - a POST with an Origin must come from that same origin.
//   - one value is taken, then the server stops. It stops after secretPageTimeout too.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// secretAsk says what tifa needs, for the page and the terminal.
type secretAsk struct {
	Title    string   // what tifa needs, short: the heading of the page
	For      string   // which app and repository or server asks, so the person knows the run
	Why      string   // one or two sentences: what it is for and where to make it
	Link     string   // where it is made, when there is such a page
	LinkText string   // the label of Link
	Fields   []string // a label for each value, asked one after the other
	Skip     string   // what an empty field does, when it may be empty
	Problem  string   // why the value sent before was refused
}

// askSecrets reads one value for each field: on a terminal with the hidden prompt, else on the page.
func askSecrets(ctx context.Context, in io.Reader, out io.Writer, ask secretAsk) ([]string, error) {
	if !stdinIsTerminal(in) {
		return askOnPage(ctx, out, ask)
	}
	values := make([]string, len(ask.Fields))
	for i, label := range ask.Fields {
		v, err := promptSecret(in, out, label)
		if err != nil {
			return nil, err
		}
		values[i] = v
		if i == 0 && ask.Skip != "" && strings.TrimSpace(v) == "" {
			// An empty first value skips: the rest is not asked.
			break
		}
	}
	return values, nil
}

// secretPageTimeout is how long the page waits for its value. Tests shorten it.
var secretPageTimeout = 10 * time.Minute

// openBrowser starts the opener of this machine on a URL. The channel gets what the opener exited
// with. Tests replace it.
var openBrowser = func(u string) (<-chan error, error) {
	args := browserCommand(runtime.GOOS, u)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Buffered, so the goroutine ends with the opener even when nobody reads it any more.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}

// listenPage listens for the page. Tests replace it.
var listenPage = func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }

const noBrowser = "could not open a browser: open the address above yourself"

// browserCommand is the opener of each system, with the URL as one argument. On Windows rundll32
// hands it to the default browser without cmd, which would read & and ^ in a URL.
func browserCommand(goos, u string) []string {
	switch goos {
	case "darwin":
		return []string{"open", u}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", u}
	default:
		return []string{"xdg-open", u}
	}
}

// askOnPage serves the page until one value comes, the wait runs out or ctx ends.
func askOnPage(ctx context.Context, out io.Writer, ask secretAsk) ([]string, error) {
	ln, err := listenPage()
	if err != nil {
		return nil, fmt.Errorf("serving the page to enter %s on: %w", ask.Title, err)
	}
	code, err := pageCode()
	if err != nil {
		ln.Close()
		return nil, err
	}
	page := newSecretPage(ask, ln.Addr().String(), code)
	srv := &http.Server{Handler: page, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	defer func() {
		// Shutdown lets the answer that says the value came finish first.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if srv.Shutdown(ctx) != nil {
			srv.Close()
		}
	}()

	u := "http://" + page.host + "/" + code
	fmt.Fprintf(out, "Tifa needs a value (%s). Open this page in a browser on this machine and enter it there:\n  %s\n", ask.Title, u)
	opened, err := openBrowser(u)
	if err != nil {
		fmt.Fprintln(out, noBrowser)
	}
	fmt.Fprintf(out, "Waiting for the page, for up to %s...\n", waitText(secretPageTimeout))

	timer := time.NewTimer(secretPageTimeout)
	defer timer.Stop()
	for {
		select {
		case values := <-page.got:
			fmt.Fprintln(out, "Received from the page.")
			return values, nil
		case err := <-opened:
			if err != nil {
				fmt.Fprintln(out, noBrowser)
			}
			opened = nil
		case err := <-served:
			return nil, fmt.Errorf("serving the page to enter %s on: %w", ask.Title, err)
		case <-timer.C:
			return nil, fmt.Errorf("no value came from the page within %s: run the command again for a new page", waitText(secretPageTimeout))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// pageCode is 128 random bits in base64url, the path of the page.
func pageCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func waitText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		n := int(d / time.Minute)
		return fmt.Sprintf("%d %s", n, plural(n, "minute", "minutes"))
	}
	return d.String()
}

func fieldName(i int) string { return fmt.Sprintf("v%d", i) }

// secretPage is the handler of the page: it answers only at its path and Host, and takes one
// value.
type secretPage struct {
	ask  secretAsk
	host string // 127.0.0.1:<port>
	path string // "/" and the code
	got  chan []string
	mu   sync.Mutex
	done bool
}

func newSecretPage(ask secretAsk, host, code string) *secretPage {
	return &secretPage{ask: ask, host: host, path: "/" + code, got: make(chan []string, 1)}
}

//go:embed secretpage.html
var secretPageHTML string

// secretPageIcon is assets/brand/tifa-icon.svg, the favicon of the page.
//
//go:embed secretpage-icon.svg
var secretPageIcon []byte

var secretPageTemplate = template.Must(template.New("page").Parse(secretPageHTML))

// secretPageScript shows and hides a value. The policy of the page allows this script by its hash
// and no other. Without it the page works, with the values hidden.
const secretPageScript = `document.querySelectorAll("[data-toggle]").forEach(function (b) {
  var input = document.getElementById(b.getAttribute("aria-controls"));
  b.hidden = false;
  b.addEventListener("click", function () {
    var show = input.type === "password";
    input.type = show ? "text" : "password";
    b.textContent = show ? "Hide" : "Show";
    input.focus();
  });
});`

var secretPagePolicy = func() string {
	sum := sha256.Sum256([]byte(secretPageScript))
	return "default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src 'sha256-" +
		base64.StdEncoding.EncodeToString(sum[:]) + "'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
}()

var secretPageIconURL = template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(secretPageIcon))

type secretPageData struct {
	secretAsk
	Fields []pageField
	Done   bool
	Again  bool // the value came before this request
	Icon   template.URL
	Script template.JS
}

type pageField struct{ Name, Label string }

func (p *secretPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(p.path)) != 1 {
		http.NotFound(w, r)
		return
	}
	if r.Host != p.host {
		http.Error(w, "This page answers only at http://"+p.host, http.StatusForbidden)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	// same-origin, not no-referrer: the code stays out of the Referer of the link to the page
	// where the secret is made, and the form still sends its Origin. With no-referrer a browser
	// sends Origin: null on the POST, which the check below refuses.
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", secretPagePolicy)
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		p.mu.Lock()
		done := p.done
		p.mu.Unlock()
		if done {
			p.render(w, http.StatusGone, secretPageData{secretAsk: secretAsk{For: p.ask.For}, Done: true, Again: true})
			return
		}
		p.render(w, http.StatusOK, p.form())
	case http.MethodPost:
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+p.host {
			http.Error(w, "Only this page may send its form", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "The form could not be read", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		if p.done {
			p.mu.Unlock()
			p.render(w, http.StatusGone, secretPageData{secretAsk: secretAsk{For: p.ask.For}, Done: true, Again: true})
			return
		}
		p.done = true
		p.mu.Unlock()
		values := make([]string, len(p.ask.Fields))
		for i := range values {
			values[i] = r.PostForm.Get(fieldName(i))
		}
		p.render(w, http.StatusOK, secretPageData{secretAsk: secretAsk{For: p.ask.For}, Done: true})
		p.got <- values
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *secretPage) form() secretPageData {
	d := secretPageData{secretAsk: p.ask}
	for i, label := range p.ask.Fields {
		d.Fields = append(d.Fields, pageField{Name: fieldName(i), Label: label})
	}
	return d
}

func (p *secretPage) render(w http.ResponseWriter, status int, d secretPageData) {
	d.Icon, d.Script = secretPageIconURL, template.JS(secretPageScript)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// An error here is a browser that went away: there is nobody left to tell.
	_ = secretPageTemplate.Execute(w, d)
}
