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
	limit int64
	loads map[uintptr]*loadCall
}

// New builds the context for one request. pattern is the matched route pattern, empty when no route matched.
// The request body is limited to bodyLimit bytes unless SetBodyLimit changes it before the first read.
func New(w http.ResponseWriter, r *http.Request, pattern string, bodyLimit int64) *Ctx {
	c := &Ctx{w: w, r: r, pattern: pattern, req: &request{limit: bodyLimit, loads: map[uintptr]*loadCall{}}}
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = &limitedBody{c: c, body: r.Body}
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
func (c *Ctx) SetContext(ctx context.Context) { c.r = c.r.WithContext(ctx) }

func (c *Ctx) Param(name string) string  { return c.r.PathValue(name) }
func (c *Ctx) Query(name string) string  { return c.r.URL.Query().Get(name) }
func (c *Ctx) Header(name string) string { return c.r.Header.Get(name) }

func (c *Ctx) Cookie(name string) (*http.Cookie, error) { return c.r.Cookie(name) }

// Pattern is the matched route pattern, empty when the URL matched no route.
func (c *Ctx) Pattern() string { return c.pattern }

// IsData reports whether this is a client navigation asking for route data.
func (c *Ctx) IsData() bool { return c.r.Header.Get("X-Tifa-Route") != "" }

// SetBodyLimit changes the request body limit. It only takes effect before the body is first read.
func (c *Ctx) SetBodyLimit(n int64) {
	c.req.mu.Lock()
	c.req.limit = n
	c.req.mu.Unlock()
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

// Redirect returns an error value. Returning it from Load, OnXxx or OnRequest makes the server redirect.
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

// limitedBody applies the body limit at the first read, so OnRequest can still change it.
type limitedBody struct {
	c    *Ctx
	body io.ReadCloser
	r    io.ReadCloser
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.r == nil {
		b.c.req.mu.Lock()
		limit := b.c.req.limit
		b.c.req.mu.Unlock()
		b.r = http.MaxBytesReader(b.c.w, b.body, limit)
	}
	return b.r.Read(p)
}

func (b *limitedBody) Close() error { return b.body.Close() }
