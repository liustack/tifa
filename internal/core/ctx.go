// Package core holds the request context behind the public tifa package.
// The server package builds contexts through the functions here, which application code cannot import.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type mode int

const (
	modeHandler mode = iota
	modeMiddleware
	modeLoad
)

// Ctx wraps one request and its response.
type Ctx struct {
	w       http.ResponseWriter
	r       *http.Request
	pattern string
	req     *request
	mode    mode
	next    func() error
	load    uintptr // key of the Load running on this context, 0 outside Load
}

// request is shared by every Ctx of the same HTTP request, including the copies handed to concurrent Loads.
type request struct {
	mu    sync.Mutex
	body  *watchedBody // nil when the request has no body
	loads map[uintptr]*loadCall
	// copies made by SetContext. net/http only removes the multipart temp files of the original request.
	copies []*http.Request
}

// New builds the context for one request. pattern is the matched route pattern, empty when no route matched.
// The request body is limited to bodyLimit bytes unless SetBodyLimit changes it before the first read.
func New(w http.ResponseWriter, r *http.Request, pattern string, bodyLimit int64) *Ctx {
	c := &Ctx{w: w, r: r, pattern: pattern, req: &request{loads: map[uintptr]*loadCall{}}}
	if r.Body != nil && r.Body != http.NoBody {
		c.req.body = &watchedBody{ReadCloser: r.Body}
		r.Body = http.MaxBytesReader(w, c.req.body, bodyLimit)
	}
	return c
}

// SetNext turns c into the context of OnRequest. Ctx.Next runs next once.
func SetNext(c *Ctx, next func() error) {
	c.mode = modeMiddleware
	c.next = next
}

func (c *Ctx) Context() context.Context { return c.r.Context() }

// SetContext replaces the request context, for middleware that stores request-scoped values.
func (c *Ctx) SetContext(ctx context.Context) {
	c.r = c.r.WithContext(ctx)
	c.req.mu.Lock()
	c.req.copies = append(c.req.copies, c.r)
	c.req.mu.Unlock()
}

// Finish releases what the request copies hold. The server calls it once the request is done.
func Finish(c *Ctx) error {
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	var errs []error
	for _, r := range c.req.copies {
		if r.MultipartForm != nil {
			errs = append(errs, r.MultipartForm.RemoveAll())
		}
	}
	return errors.Join(errs...)
}

func (c *Ctx) Param(name string) string  { return c.r.PathValue(name) }
func (c *Ctx) Query(name string) string  { return c.r.URL.Query().Get(name) }
func (c *Ctx) Header(name string) string { return c.r.Header.Get(name) }

func (c *Ctx) Cookie(name string) (*http.Cookie, error) { return c.r.Cookie(name) }

// Pattern is the matched route pattern, empty when the URL matched no route.
func (c *Ctx) Pattern() string { return c.pattern }

// IsData reports whether this is a client navigation asking for route data.
func (c *Ctx) IsData() bool { return c.r.Header.Get("X-Tifa-Route") != "" }

// SetBodyLimit changes the request body limit. Call it before anything reads the body, on the goroutine
// that handles the request, usually in OnRequest. Once reading has started it returns an error and the
// limit stays as it was. The body stays an http.MaxBytesReader, so ParseForm honours the limit instead
// of its own 10 MiB cap.
func (c *Ctx) SetBodyLimit(n int64) error {
	if c.req.body == nil {
		return nil
	}
	if c.req.body.started {
		return errors.New("tifa: SetBodyLimit called after the request body was read")
	}
	c.r.Body = http.MaxBytesReader(c.w, c.req.body, n)
	return nil
}

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
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.WriteHeader(code)
	_, err = c.w.Write(data)
	return err
}

func (c *Ctx) Text(code int, s string) error {
	if err := c.checkWrite(); err != nil {
		return err
	}
	c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
	if c.mode == modeLoad {
		return errors.New("tifa: Load cannot write the response body, it may only set headers and cookies")
	}
	return nil
}

// Redirect returns an error value. Returning it from Load, a method handler or OnRequest makes the server redirect.
func (c *Ctx) Redirect(url string, code int) error {
	if code < 300 || code > 399 {
		return fmt.Errorf("tifa: redirect to %s with non-3xx status %d", url, code)
	}
	return &RedirectError{URL: url, Code: code}
}

// Next runs the matched route from OnRequest and returns its error. It may be called once.
func (c *Ctx) Next() error {
	if c.mode != modeMiddleware {
		return errors.New("tifa: Next can only be called from OnRequest")
	}
	if c.next == nil {
		return errors.New("tifa: Next called more than once")
	}
	next := c.next
	c.next = nil
	c.mode = modeHandler
	defer func() { c.mode = modeMiddleware }()
	return next()
}

func (c *Ctx) Request() *http.Request      { return c.r }
func (c *Ctx) Writer() http.ResponseWriter { return c.w }

// watchedBody tells SetBodyLimit whether reading has started. The first Read that reaches it counts, whatever it
// returns. http.MaxBytesReader answers zero-length reads itself, and those consume nothing.
type watchedBody struct {
	io.ReadCloser
	started bool
}

func (b *watchedBody) Read(p []byte) (int, error) {
	b.started = true
	return b.ReadCloser.Read(p)
}
