package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

func TestRenderURLKeepsTheParametersTheLoadsRead(t *testing.T) {
	for _, tc := range []struct {
		target string
		keep   []string
		want   string
	}{
		{"/news?utm_source=a&page=2", []string{"page"}, "/news?page=2"},
		{"/news?utm_source=a&page=2", nil, "/news"},
		{"/news", []string{"page"}, "/news"},
		{"/news?utm_source=a", []string{"page"}, "/news"},
		// Sorted by name, the values of one name in the order of the request.
		{"/s?b=2&x=1&a=1&b=3", []string{"a", "b"}, "/s?a=1&b=2&b=3"},
		{"/s?q=b&x=2&q=c", []string{"q"}, "/s?q=b&q=c"},
		// Spelled the way url.QueryEscape writes it, which parses back to the same value.
		{"/s?q=a+b%26c&q=%2F", []string{"q"}, "/s?q=a+b%26c&q=%2F"},
		{"/s?q=&r", []string{"q", "r"}, "/s?q=&r="},
		// What url.ParseQuery drops, c.Query does not see either.
		{"/s?q=%zz&q=1&r;s=1", []string{"q", "r"}, "/s?q=1"},
		// An empty part is no parameter, as url.ParseQuery skips it, and an empty name is one.
		{"/s?", []string{""}, "/s"},
		{"/s?&", []string{""}, "/s"},
		{"/s?&&", []string{"", "q"}, "/s"},
		{"/s?&q=1&&q=2&", []string{"q"}, "/s?q=1&q=2"},
		{"/s?=x&", []string{""}, "/s?=x"},
		{"/s?=x&&=y&=", []string{""}, "/s?=x&=y&="},
		// The path stays as it came, so the router can redirect another spelling of it.
		{"/%6Eews?page=2", []string{"page"}, "/%6Eews?page=2"},
	} {
		u, err := url.ParseRequestURI(tc.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := renderURL(u, tc.keep); got != tc.want {
			t.Errorf("renderURL(%q, %q) = %q, want %q", tc.target, tc.keep, got, tc.want)
		}
	}
}

// queryApp has a page whose Load reads sort and whose Layout reads page, and a page that reads none.
func queryApp() App {
	return App{
		Layouts: []Layout{
			{ID: "app/layout", Path: "/", SSR: true, Query: []string{"page"},
				Load: func(c *tifa.Ctx) (any, error) { return c.Query("page"), nil }},
		},
		Routes: []Route{
			{ID: "app/list", Path: "/list", Page: true, SSR: true, Layouts: []string{"app/layout"}, Query: []string{"sort"},
				Load: func(c *tifa.Ctx) (any, error) { return c.Query("sort"), nil }},
			{ID: "app/plain", Path: "/plain", Page: true, SSR: true, Layouts: []string{"app/layout"}},
		},
	}
}

func TestServerRenderSeesOnlyTheQueryItsLoadsRead(t *testing.T) {
	s, r := newFrontServer(t, queryApp())
	w := get(s, "/list?utm_source=mail&sort=name&x=1&page=2")
	if w.Code != 200 || r.count() != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if in := r.inputs[0]; in.URL != "/list?page=2&sort=name" {
		t.Errorf("render URL %q", in.URL)
	}
	// Other parameters, and another order, give the same page from the render memo.
	for _, target := range []string{"/list?sort=name&page=2", "/list?page=2&fbclid=b&sort=name&x=2"} {
		w2 := get(s, target)
		if w2.Code != 200 || w2.Header().Get("X-Tifa-Render") != "memo" || w2.Header().Get("ETag") != w.Header().Get("ETag") {
			t.Errorf("%s: %d %v", target, w2.Code, w2.Header())
		}
	}
	// A parameter the Loads read is another page.
	if w3 := get(s, "/list?page=3&sort=name"); w3.Header().Get("X-Tifa-Render") != "vm" || r.inputs[len(r.inputs)-1].URL != "/list?page=3&sort=name" {
		t.Errorf("page 3: %v", w3.Header())
	}
	get(s, "/plain?sort=x&page=1&y=2")
	if in := r.inputs[len(r.inputs)-1]; in.URL != "/plain?page=1" {
		t.Errorf("render URL of the page without a Load %q", in.URL)
	}
	if entries, _ := s.memo.stats(); entries.Entries != 3 {
		t.Errorf("memo %+v", entries)
	}
}

// The router redirects another spelling of a URL to the one it builds from the URL it rendered,
// which lacks the parameters the Loads do not read: the redirect puts them back as they came.
func TestCanonicalRedirectKeepsTheDroppedParameters(t *testing.T) {
	s, r := newFrontServer(t, queryApp())
	// What the router does: it builds the URL from the path and the query it was given.
	r.result = func(in ssr.Input) (string, error) {
		u, err := url.ParseRequestURI(in.URL)
		if err != nil {
			return "", err
		}
		clean := strings.TrimSuffix(strings.ReplaceAll(u.EscapedPath(), "%6C", "l"), "/")
		if clean != u.EscapedPath() {
			if u.RawQuery != "" {
				clean += "?" + u.RawQuery
			}
			return "", &ssr.RedirectError{Location: clean, Status: http.StatusPermanentRedirect}
		}
		return "<html>" + in.URL + "</html>", nil
	}
	for _, tc := range []struct{ target, want string }{
		{"/%6Cist?utm_source=mail&page=2&sort=name", "/list?page=2&sort=name&utm_source=mail"},
		// The second request gets the redirect from the render memo, with its own parameters.
		{"/%6Cist?page=2&sort=name&fbclid=a%20b&x", "/list?page=2&sort=name&fbclid=a%20b&x"},
		{"/%6Cist?page=2&sort=name", "/list?page=2&sort=name"},
		// A URL that matches no page renders the 404 inside the root Layout, which reads page.
		{"/list/?utm_source=mail&page=2", "/list?page=2&utm_source=mail"},
		{"/plain/?sort=a", "/plain?sort=a"},
	} {
		w := get(s, tc.target)
		if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != tc.want {
			t.Errorf("%s: %d %q, want %q", tc.target, w.Code, w.Header().Get("Location"), tc.want)
		}
	}
}
