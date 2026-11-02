package tifa_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// App code and its tests must be able to name the error that Redirect returns.
func TestRedirectErrorMatchesWithErrorsAs(t *testing.T) {
	err := tifa.Redirect("/login", http.StatusSeeOther)
	var redirect *tifa.RedirectError
	if !errors.As(err, &redirect) {
		t.Fatalf("errors.As did not find a *tifa.RedirectError in %v", err)
	}
	if redirect.URL != "/login" || redirect.Status != http.StatusSeeOther {
		t.Fatalf("got %q %d", redirect.URL, redirect.Status)
	}
	wrapped := fmt.Errorf("check: %w", err)
	if !errors.As(wrapped, &redirect) {
		t.Fatalf("errors.As did not find the redirect in %v", wrapped)
	}
}

// A status that is not 3xx is a mistake of the programmer, and Redirect panics like
// http.ResponseWriter.WriteHeader does for a code it cannot send.
func TestRedirectPanicsOnANon3xxStatus(t *testing.T) {
	for _, status := range []int{0, 200, 299, 400, 500} {
		func() {
			defer func() {
				msg := fmt.Sprint(recover())
				if !strings.Contains(msg, "Redirect") || !strings.Contains(msg, fmt.Sprint(status)) {
					t.Errorf("status %d: recovered %q", status, msg)
				}
			}()
			tifa.Redirect("/login", status)
			t.Errorf("status %d did not panic", status)
		}()
	}
}
