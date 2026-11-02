// Package login is the sign-in page of the workbench.
package login

import (
	"net/url"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop"
)

// View is what the form needs: where to go after signing in.
type View struct {
	Next string `json:"next"`
}

func Load(c *tifa.Ctx) (View, error) {
	return View{Next: safeNext(c.Query("next"))}, nil
}

// Credentials is the form.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Next     string `json:"next"`
}

// POST signs the visitor in and sends them back to the page they came from.
func POST(c *tifa.Ctx, in Credentials) error {
	s, ok := shop.CheckPassword(in.Email, in.Password)
	if !ok {
		return tifa.Error{Status: 401, Message: "邮箱或密码不对"}
	}
	if err := tifa.SetSession(c, &s); err != nil {
		return err
	}
	return c.Redirect(safeNext(in.Next), 303)
}

// safeNext keeps the address on this site, and sends everything else to the workbench. It must be
// a path with a single leading slash, no scheme, host or user, no control character or backslash
// (a browser drops tabs and line breaks from a URL and reads a backslash as a slash, so "/\n/host"
// would leave the site), and no encoded slash or backslash in the path. The query is kept.
func safeNext(next string) string {
	if next == "" || strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return home
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return home
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || u.Path == "/login" ||
		strings.ContainsFunc(u.Path, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return home
	}
	return next
}

const home = "/projects/studio"
