package login

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/tifatest"
)

// nexts are the values of next and where each goes. Only a path of this site is kept, with its
// query. Control characters, backslashes, a second slash and anything with a scheme or a host fall
// back to the workbench. A browser strips tabs and line breaks from a URL, so "/\n/evil" is a
// network-path reference for it.
var nexts = map[string]string{
	"/projects/studio/orders/SH-1042":         "/projects/studio/orders/SH-1042",
	"/projects/studio/stats?range=7d&a=b%20c": "/projects/studio/stats?range=7d&a=b%20c",
	"/:@evil.example":                         "/:@evil.example",
	"":                                        home,
	"https://evil.example/":                   home,
	"//evil.example/":                         home,
	`/\evil.example/`:                         home,
	"/%5Cevil.example/":                       home,
	"/%2Fevil.example/":                       home,
	"%2F%2Fevil.example/":                     home,
	"/%0A/evil.example/":                      home,
	"/%0D/evil.example/":                      home,
	"/%09/evil.example/":                      home,
	"/\n/evil.example/":                       home,
	"/\r/evil.example/":                       home,
	"/\t/evil.example/":                       home,
	"\n//evil.example/":                       home,
	"/projects\n":                             home,
	"/projects?x=\n":                          home,
	"/\x00":                                   home,
	"/\x7f":                                   home,
	"/login":                                  home,
	"/login?next=/projects":                   home,
	"javascript:alert(1)":                     home,
}

func TestLoadKeepsNextOnThisSite(t *testing.T) {
	for next, want := range nexts {
		r := httptest.NewRequest("GET", "/login", nil)
		r.URL.RawQuery = "next=" + url.QueryEscape(next)
		c, _ := tifatest.NewCtx(t, r)
		v, err := tifa.Use(c, Load)
		if err != nil || v.Next != want {
			t.Errorf("Load next %q: %q %v, want %q", next, v.Next, err, want)
		}
	}
}

func TestPOSTRedirectKeepsNextOnThisSite(t *testing.T) {
	for next, want := range nexts {
		c, _ := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
		err := POST(c, Credentials{Email: shop.DemoEmail, Password: shop.DemoPassword, Next: next})
		var r *tifa.RedirectError
		if !errors.As(err, &r) || r.URL != want || r.Status != 303 {
			t.Errorf("POST next %q: %v, want a redirect to %q", next, err, want)
		}
	}
}

func TestPOSTSignsInAndGoesBack(t *testing.T) {
	c, rec := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	err := POST(c, Credentials{Email: shop.DemoEmail, Password: shop.DemoPassword, Next: "/projects/weekend"})
	var r *tifa.RedirectError
	if !errors.As(err, &r) || r.URL != "/projects/weekend" || r.Status != 303 {
		t.Fatalf("POST = %v", err)
	}
	if s := tifa.Session[shop.Session](c); s == nil || s.Name != "顾遥" {
		t.Errorf("session = %+v", s)
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "_session=") {
		t.Errorf("Set-Cookie = %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestPOSTRefusesAWrongPassword(t *testing.T) {
	c, rec := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	err := POST(c, Credentials{Email: shop.DemoEmail, Password: "nope"})
	var e tifa.Error
	if !errors.As(err, &e) || e.Status != 401 {
		t.Fatalf("POST = %v", err)
	}
	if rec.Header().Get("Set-Cookie") != "" || tifa.Session[shop.Session](c) != nil {
		t.Error("a wrong password signed in")
	}
}
