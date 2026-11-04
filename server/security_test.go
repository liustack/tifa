package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
)

// defaultHeaders are the headers every answer carries whatever the app does, as long as it does
// not change them: none of them changes what a page loads or how it works.
var defaultHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Referrer-Policy":         "strict-origin-when-cross-origin",
	"Content-Security-Policy": "frame-ancestors 'self'",
}

func hasDefaultHeaders(t *testing.T, what string, h http.Header) {
	t.Helper()
	for name, want := range defaultHeaders {
		if got := h.Values(name); len(got) != 1 || got[0] != want {
			t.Errorf("%s: %s is %q, want %q", what, name, got, want)
		}
	}
}

// The headers go on every answer: a page, JSON of a handler, an error, a URL that matches nothing,
// a public file and a write refused as cross-site, which no app code sees.
func TestDefaultSecurityHeaders(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	w := get(s, "/projects/p1")
	if w.Code != 200 {
		t.Fatalf("page: %d", w.Code)
	}
	hasDefaultHeaders(t, "page", w.Header())
	hasDefaultHeaders(t, "navigation data", get(s, "/projects/p1?_tifa=").Header())
	hasDefaultHeaders(t, "404 page", get(s, "/nothing/here").Header())

	api := newTestServer(Hooks{})
	hasDefaultHeaders(t, "JSON", get(api, "/orders/1").Header())
	hasDefaultHeaders(t, "error", get(api, "/orders/broken").Header())

	hasDefaultHeaders(t, "public file", get(publicServer(t, true), "/robots.txt").Header())

	r := httptest.NewRequest("POST", "/orders/1", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	refused := httptest.NewRecorder()
	api.ServeHTTP(refused, r)
	if refused.Code != 403 {
		t.Fatalf("cross-site write: %d", refused.Code)
	}
	hasDefaultHeaders(t, "cross-site write", refused.Header())
}

// The headers are set before OnRequest, so an app changes them there for the paths it chooses,
// such as pages another site may frame.
func TestOnRequestChangesTheDefaultHeaders(t *testing.T) {
	s := newTestServer(Hooks{OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
		c.Writer().Header().Del("Referrer-Policy")
		return next(c)
	}})
	h := get(s, "/orders/1").Header()
	if got := h.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "frame-ancestors https://partner.example" {
		t.Errorf("Content-Security-Policy %q", got)
	}
	if got := h.Values("Referrer-Policy"); len(got) != 0 {
		t.Errorf("Referrer-Policy %q", got)
	}
}
