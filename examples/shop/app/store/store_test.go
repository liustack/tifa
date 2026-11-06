package store

import (
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa/tifatest"
)

func TestStoreEntryRedirectsToALanguage(t *testing.T) {
	r := httptest.NewRequest("GET", "/store", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	c, w := tifatest.NewCtx(t, r)
	if err := GET(c); err == nil || err.Error() != "tifa: redirect 302 to /store/en-US" {
		t.Errorf("got %v, want a redirect to /store/en-US", err)
	}
	if w.Header().Get("Vary") != "Accept-Language" {
		t.Errorf("Vary %q", w.Header().Get("Vary"))
	}
}
