// Package core holds the request context behind the public tifa package.
// The server package builds contexts through the functions here, which application code cannot import.
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Ctx wraps one request and its response.
type Ctx struct {
	w       http.ResponseWriter
	r       *http.Request
	pattern string
	req     *request
	run     *loadCall // the run of a Load or Layout function this Ctx was made for, nil outside one
}

// request is shared by every Ctx of the same HTTP request, including the copies handed to concurrent Loads.
type request struct {
	mu    sync.Mutex
	body  *watchedBody  // nil when the request has no body
	orig  *http.Request // the request the server got, whose Body SetBodyLimit sets with the one of each copy
	loads map[uintptr]*loadCall
	// loadsStart is when the first Load of the request started, zero before one did (see StartLoads).
	loadsStart time.Time
	// requests are the request the Ctx was made with and the copies made by WithContext. Each may
	// hold multipart temp files. net/http only removes those of the request it made itself, which
	// is not the first one here when the server or a middleware handed a copy to New.
	requests []*http.Request
	sess     sessionState
	// liftRead moves the read deadline of the connection, and the write deadline after it, see
	// SetReadTimeout. The server sets it, a Ctx made by a test has none.
	liftRead func(d time.Duration) error
	// language is the language of the route the request matched, see Language.
	language string
	// alternates is the list SetAlternates gave, set tells it from a page that gave none.
	alternates []Alternate
	altSet     bool
	// views are the views of the Load and Layout functions of the app by code pointer, see
	// SetLoadViews.
	views map[uintptr]*LoadView
}

// New builds the context for one request. pattern is the matched route pattern, empty when no route matched.
// The request body is limited to bodyLimit bytes unless SetBodyLimit changes it before the first read.
func New(w http.ResponseWriter, r *http.Request, pattern string, bodyLimit int64) *Ctx {
	c := &Ctx{w: w, r: r, pattern: pattern, req: &request{orig: r, requests: []*http.Request{r}, loads: map[uintptr]*loadCall{}}}
	if r.Body != nil && r.Body != http.NoBody {
		c.req.body = &watchedBody{ReadCloser: r.Body}
		r.Body = http.MaxBytesReader(w, c.req.body, bodyLimit)
	}
	return c
}

func (c *Ctx) Context() context.Context { return c.r.Context() }

// WithContext returns a shallow copy of c whose request carries ctx, as http.Request.WithContext does.
// c is not changed. Both share the response and the state of the request: Loads run once for either,
// and the multipart files of the copy are removed when the request ends.
func (c *Ctx) WithContext(ctx context.Context) *Ctx {
	d := *c
	// Copying the request reads its Body, which SetBodyLimit sets under the lock. Copying and
	// registering in one critical section means a copy either has the new reader or is registered
	// in time to get it.
	c.req.mu.Lock()
	d.r = c.r.WithContext(ctx)
	c.req.requests = append(c.req.requests, d.r)
	c.req.mu.Unlock()
	return &d
}

// Finish releases the multipart files the request and its copies hold. The server calls it once the request is done.
func Finish(c *Ctx) error {
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	var errs []error
	for _, r := range c.req.requests {
		if r.MultipartForm != nil {
			errs = append(errs, r.MultipartForm.RemoveAll())
		}
	}
	return errors.Join(errs...)
}

// Param returns the value of the path parameter name. In a Layout function it is "" for a
// parameter of a folder below the Layout, as in the copy Request returns there.
func (c *Ctx) Param(name string) string {
	if c.run != nil && c.run.view != nil && slices.Contains(c.run.view.Hide, name) {
		return ""
	}
	return c.r.PathValue(name)
}

// Query returns the first value of the query parameter name, from the request Request returns: in
// a Load or Layout function, only the query parameters it declared are there.
func (c *Ctx) Query(name string) string { return c.Request().URL.Query().Get(name) }

func (c *Ctx) Header(name string) string { return c.r.Header.Get(name) }

// QueryValues returns every value of the query parameter name, in order, and nil when there is
// none. Query returns only the first.
func (c *Ctx) QueryValues(name string) []string { return c.Request().URL.Query()[name] }

func (c *Ctx) Cookie(name string) (*http.Cookie, error) { return c.r.Cookie(name) }

// Pattern is the matched route pattern, empty when the URL matched no route.
func (c *Ctx) Pattern() string { return c.pattern }

// Path is the path of the page as the visitor sees it in the address bar: escaped the way the
// browser writes it, with the language prefix and without the query. The page load and the data
// request of a client navigation give the same path, and a handler gets its own path.
func (c *Ctx) Path() string { return c.r.URL.EscapedPath() }

// IsDataRequest reports whether this is a client navigation asking for route data.
func (c *Ctx) IsDataRequest() bool { return DataOf(c.r) != nil }

// SetBodyLimit changes the request body limit. Call it before anything reads the body, on the goroutine
// that handles the request, usually in OnRequest. Once reading has started it returns an error and the
// limit stays as it was. The body stays an http.MaxBytesReader, so ParseForm honours the limit instead
// of its own 10 MiB cap.
func (c *Ctx) SetBodyLimit(n int64) error {
	if c.req.body == nil {
		return nil
	}
	if c.req.body.started.Load() {
		return errors.New("tifa: SetBodyLimit called after the request body was read")
	}
	// Every copy made by WithContext reads the same body, so each gets the new reader. A MaxBytesReader
	// itself, and not a wrapper, because ParseForm looks for one to leave out its own cap.
	body := http.MaxBytesReader(c.w, c.req.body, n)
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	c.req.orig.Body = body
	for _, r := range c.req.requests {
		r.Body = body
	}
	c.r.Body = body
	return nil
}

// SetReadTimeout gives the body of this request d to arrive, in place of server.read_timeout, and
// the answer server.write_timeout after that. Call it before anything reads the body, on the
// goroutine that handles the request, usually in OnRequest. Once reading has started it returns an error and the deadline stays as it
// was. It also returns an error when d is not over 0 or when the response cannot reach the
// connection, such as behind a middleware of SetHTTPHandler that does not unwrap it. A request
// with no body, such as a GET, keeps its deadlines and the call does nothing. A Ctx a test made
// with tifatest.NewCtx has no connection, and the call does nothing either.
func (c *Ctx) SetReadTimeout(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("tifa: SetReadTimeout needs a duration over 0, got %v", d)
	}
	// No body, no read to wait for. A deadline would only reach the background read net/http does
	// to see the client leave, and cancel the request while the client is still there.
	if c.req.body == nil {
		return nil
	}
	if c.req.body.started.Load() {
		return errors.New("tifa: SetReadTimeout called after the request body was read")
	}
	if c.req.liftRead == nil {
		return nil
	}
	return c.req.liftRead(d)
}

// SetReadLift gives c what SetReadTimeout calls to move the deadlines. The server sets it.
func SetReadLift(c *Ctx, lift func(d time.Duration) error) { c.req.liftRead = lift }

func (c *Ctx) SetHeader(key, value string) {
	c.req.mu.Lock()
	c.w.Header().Set(key, value)
	c.req.mu.Unlock()
}

func (c *Ctx) SetCookie(cookie *http.Cookie) {
	c.req.mu.Lock()
	http.SetCookie(c.w, cookie)
	c.req.mu.Unlock()
}

func (c *Ctx) JSON(code int, v any) error {
	if err := c.checkWrite(); err != nil {
		return err
	}
	data, err := MarshalJSON(v)
	if err != nil {
		return err
	}
	return c.writeJSON(code, data)
}

// JSONForPage is c.JSON for an answer a page reads (the result of a handler called from a page):
// it fails, as an unexpected error, when a browser would read a number as another one. what names
// the answer in the error.
func JSONForPage(c *Ctx, code int, v any, what string) error {
	if err := c.checkWrite(); err != nil {
		return err
	}
	data, err := MarshalForBrowser(v, what)
	if err != nil {
		return err
	}
	return c.writeJSON(code, data)
}

func (c *Ctx) writeJSON(code int, data []byte) error {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	c.w.WriteHeader(code)
	_, err := c.w.Write(data)
	return err
}

func (c *Ctx) Text(code int, s string) error {
	if err := c.checkWrite(); err != nil {
		return err
	}
	c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.w.Header().Set("Content-Length", strconv.Itoa(len(s)))
	c.w.WriteHeader(code)
	_, err := io.WriteString(c.w, s)
	return err
}

func (c *Ctx) NoContent(code int) error {
	if err := c.checkWrite(); err != nil {
		return err
	}
	c.w.WriteHeader(code)
	return nil
}

func (c *Ctx) checkWrite() error {
	if c.run != nil {
		return errors.New("tifa: Load cannot write the response body, it may only set headers and cookies")
	}
	return nil
}

// Request returns the request. In a Load or Layout function, and the functions it calls, it is a
// copy made on the first call of the run, which holds what the function may depend on: the query
// parameters it declared, the path parameters of its own folder for a Layout, and no body. Each
// run has its own copy, so a function that changes it changes nothing of another.
func (c *Ctx) Request() *http.Request {
	if c.run == nil {
		return c.r
	}
	// Copying reads the Body of c.r, which SetBodyLimit sets under the lock.
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	if c.run.from != c.r {
		c.run.from, c.run.copy = c.r, c.run.view.request(c.r)
	}
	return c.run.copy
}

func (c *Ctx) Writer() http.ResponseWriter { return c.w }

// LoadView is what a Load or Layout function sees of the request besides its own parameters.
// The server makes one for each function of the app.
type LoadView struct {
	// RawQuery returns the query string of u with only the parameters the function declared.
	RawQuery func(u *url.URL) string
	// Hide lists the path parameters it does not see: for a Layout, those of the folders below it.
	Hide []string
}

// request makes the copy of r a function with the view v sees. A nil v is a function the server
// made no view for, which sees the whole query.
func (v *LoadView) request(r *http.Request) *http.Request {
	cp := r.Clone(r.Context())
	cp.Body, cp.GetBody, cp.ContentLength = http.NoBody, nil, 0
	cp.TransferEncoding, cp.Trailer = nil, nil
	cp.Form, cp.PostForm, cp.MultipartForm = nil, nil, nil
	if v != nil {
		cp.URL.RawQuery, cp.URL.ForceQuery = v.RawQuery(r.URL), false
		for _, name := range v.Hide {
			cp.SetPathValue(name, "")
		}
	}
	cp.RequestURI = cp.URL.RequestURI()
	return cp
}

// SetLoadViews gives the request the views of the Load and Layout functions of the app, by the
// code pointer of each function. The server sets them before the request reaches OnRequest.
func SetLoadViews(c *Ctx, views map[uintptr]*LoadView) { c.req.views = views }

// watchedBody tells SetBodyLimit whether reading has started. The first Read that reaches it counts, whatever it
// returns. http.MaxBytesReader answers zero-length reads itself, and those consume nothing.
type watchedBody struct {
	io.ReadCloser
	started atomic.Bool // set by the goroutine that reads, read by SetBodyLimit and SetReadTimeout
}

func (b *watchedBody) Read(p []byte) (int, error) {
	b.started.Store(true)
	return b.ReadCloser.Read(p)
}

// Language returns the language of the route the request is for: the language in the URL, the
// default language when the URL has none, and so for a route outside every area of a site with
// languages. It is empty on a site without languages.
func (c *Ctx) Language() string { return c.req.language }

// SetLanguage sets what Language returns. The server sets it before the request reaches OnRequest.
func SetLanguage(c *Ctx, language string) { c.req.language = language }

// Alternate is one version of a page in a language. Path is the URL path of that version as a
// visitor sees it, escaped, with no query and no language prefix: tifa adds the prefix of the
// language.
type Alternate struct {
	Language string
	Path     string
}

// SetAlternates replaces the versions of the page in other languages. Without it a page exists in
// every language of its area, at the same path under another prefix. Call it in a Load of a page
// where that is wrong: a content entry whose slug differs in another language, or that is missing
// in one. Include the page itself. The last call wins, and an empty list says the page has one
// version.
func (c *Ctx) SetAlternates(list []Alternate) {
	c.req.mu.Lock()
	c.req.alternates, c.req.altSet = slices.Clone(list), true
	c.req.mu.Unlock()
}

// AlternatesOf returns the list SetAlternates gave, and whether it was called. The server reads
// it once the Loads of a page have returned.
func AlternatesOf(c *Ctx) ([]Alternate, bool) {
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	return c.req.alternates, c.req.altSet
}
