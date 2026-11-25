package shop

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

// request runs OnRequest for path, which the route pattern matched.
func request(t *testing.T, path, pattern string, signedIn bool) (redirect string, reached bool) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Pattern = pattern
	c, _ := tifatest.NewCtx(t, req)
	if signedIn {
		tifatest.SignIn(t, c, &Session{UserID: "u-1", Name: "顾遥", Initials: "顾"})
	}
	err := OnRequest(c, func(*tifa.Ctx) error { reached = true; return nil })
	var r *tifa.RedirectError
	if errors.As(err, &r) {
		return r.URL, reached
	}
	if err != nil {
		t.Fatal(err)
	}
	return "", reached
}

func TestWorkbenchNeedsSignIn(t *testing.T) {
	for _, tc := range []struct {
		path, pattern, redirect string
		signedIn                bool
	}{
		{"/projects/studio", "/projects/{project}", "/login?next=%2Fprojects%2Fstudio", false},
		{"/projects/studio/orders/SH-1042?tab=items", "/projects/{project}/orders/{order}", "/login?next=%2Fprojects%2Fstudio%2Forders%2FSH-1042%3Ftab%3Ditems", false},
		{"/projects/studio", "/projects/{project}", "", true},
		// Another spelling of the same route is turned away as the plain one is.
		{"/%70rojects", "/projects", "/login?next=%2F%2570rojects", false},
		{"/%70rojects/studio", "/projects/{project}", "/login?next=%2F%2570rojects%2Fstudio", false},
		{"/zh-CN/store", "/store", "", false},
		{"/login", "/login", "", false},
		{"/projectsx", "", "", false},
		{"/metrics", "/metrics", "/login?next=%2Fmetrics", false},
		{"/metrics", "/metrics", "", true},
	} {
		redirect, reached := request(t, tc.path, tc.pattern, tc.signedIn)
		if redirect != tc.redirect || reached != (tc.redirect == "") {
			t.Errorf("%s signed in %v: redirect %q, reached %v", tc.path, tc.signedIn, redirect, reached)
		}
	}
}

func TestOpenWorkbenchSkipsSignIn(t *testing.T) {
	Open = true
	t.Cleanup(func() { Open = false })
	if redirect, reached := request(t, "/projects/studio", "/projects/{project}", false); redirect != "" || !reached {
		t.Errorf("redirect %q, reached %v", redirect, reached)
	}
}

func TestCheckPassword(t *testing.T) {
	s, ok := CheckPassword(" Owner@Shanhai.example ", DemoPassword)
	if !ok || s.Name != "顾遥" || s.Exposed().Initials != "顾" {
		t.Errorf("demo account: %+v %v", s, ok)
	}
	for _, tc := range [][2]string{{DemoEmail, "wrong"}, {"nobody@shanhai.example", DemoPassword}, {"", ""}} {
		if _, ok := CheckPassword(tc[0], tc[1]); ok {
			t.Errorf("%q %q signed in", tc[0], tc[1])
		}
	}
}
