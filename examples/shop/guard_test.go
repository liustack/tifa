package shop_test

import (
	"net/http"
	"testing"

	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

// The guard asks which route matched, so another spelling of a private address is turned away
// as the plain one is: /%70rojects is /projects.
func TestGuardTurnsAwayEverySpellingOfAPrivateAddress(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	for _, path := range []string{"/projects/studio/stats", "/%70rojects/studio/stats", "/projects/studio", "/%70rojects/studio", "/metrics", "/%6detrics"} {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusFound || res.Header.Get("Location") == "" || res.Header.Get("Location")[:len("/login?next=")] != "/login?next=" {
			t.Errorf("GET %s: %d, location %q, want a redirect to /login", path, res.StatusCode, res.Header.Get("Location"))
		}
	}
}
