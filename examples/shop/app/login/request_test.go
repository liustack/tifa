package login_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

// A visitor who is not signed in is sent to the sign-in page, signs in there and is sent back to
// the workbench page they asked for, which then answers. tifatest.Start runs the whole app, so
// OnRequest, the decoding of the form and the session cookie all take part, as in a browser.
func TestSignInLeadsBackToTheWorkbench(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	client := srv.Client()

	res, err := client.Get(srv.URL + "/projects/studio/stats")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if want := "/login?next=" + url.QueryEscape("/projects/studio/stats"); res.StatusCode != 302 || res.Header.Get("Location") != want {
		t.Fatalf("the workbench before signing in: %d to %q, want 302 to %q", res.StatusCode, res.Header.Get("Location"), want)
	}

	signIn := func(password string) *http.Response {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": shop.DemoEmail, "password": password, "next": "/projects/studio/stats"})
		res, err := client.Post(srv.URL+"/login", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := signIn("wrong"); res.StatusCode != 401 || len(res.Cookies()) != 0 {
		t.Fatalf("a wrong password: %d with cookies %v", res.StatusCode, res.Cookies())
	}
	if res := signIn(shop.DemoPassword); res.StatusCode != 303 || res.Header.Get("Location") != "/projects/studio/stats" {
		t.Fatalf("signing in: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}

	// The client keeps the session cookie. The app has no front end here, so the page answers the
	// data of its Load functions by route ID.
	res, err = client.Get(srv.URL + "/projects/studio/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var data map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil || res.StatusCode != 200 {
		t.Fatalf("the workbench after signing in: %d, %v", res.StatusCode, err)
	}
	if _, ok := data["app/projects/project+/stats/route"]; !ok {
		t.Errorf("the stats page answered %s", data)
	}
}
