package tifa_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

// App code and its tests must be able to name the error that Redirect returns.
func TestRedirectErrorMatchesWithErrorsAs(t *testing.T) {
	c, _ := tifatest.NewCtx(t, httptest.NewRequest(http.MethodGet, "/", nil))
	err := c.Redirect("/login", http.StatusSeeOther)
	var redirect *tifa.RedirectError
	if !errors.As(err, &redirect) {
		t.Fatalf("errors.As did not find a *tifa.RedirectError in %v", err)
	}
	if redirect.URL != "/login" || redirect.Code != http.StatusSeeOther {
		t.Fatalf("got %q %d", redirect.URL, redirect.Code)
	}
}
