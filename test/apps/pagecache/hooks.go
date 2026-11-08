// Package pagecachetest is the root of the app that tests pages with Cache. Its hook records
// what the code of an app sees on a page with Cache, and sets a cookie on every answer.
package pagecachetest

import (
	"net/http"
	"strings"

	"github.com/liustack/tifa"

	"example.com/pagecachetest/app/login"
	"example.com/pagecachetest/internal/counter"
)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	counter.OnRequests.Add(1)
	r := c.Request()
	if strings.HasPrefix(r.URL.Path, "/news") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		_, cookieErr := c.Cookie("visit")
		s := counter.Seen{
			Path:          r.URL.Path,
			Cookie:        cookieErr == nil,
			Authorization: c.Header("Authorization") != "",
			RequestCookie: r.Header.Get("Cookie") != "",
			Session:       tifa.Session[login.Account](c) != nil,
		}
		if err := tifa.SetSession(c, &login.Account{ID: 9}); err != nil {
			s.SetSessionError = err.Error()
		}
		if err := tifa.ClearSession(c); err != nil {
			s.ClearSessionError = err.Error()
		}
		counter.SetSeen(s)
	}
	c.SetCookie(&http.Cookie{Name: "visitor", Value: "v", Path: "/"})
	return next(c)
}
