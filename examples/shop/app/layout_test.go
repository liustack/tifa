package app

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/tifatest"
)

// The sidebar component stores its state in the cookie sidebar_state, so a reload renders the
// sidebar the way the user left it. Only "false" collapses it.
func TestLayoutKeepsTheSidebarAsTheUserLeftIt(t *testing.T) {
	for _, tc := range []struct {
		cookie string
		open   bool
	}{
		{"sidebar_state=false", false},
		{"sidebar_state=true", true},
		{"sidebar_state=", true},
		{"", true},
		{"theme=dark", true},
	} {
		r := httptest.NewRequest("GET", "/projects/studio", nil)
		if tc.cookie != "" {
			r.Header.Set("Cookie", tc.cookie)
		}
		c, _ := tifatest.NewCtx(t, r)
		tifatest.SignIn(t, c, &shop.Session{UserID: "u-9", Name: "周宁", Initials: "周"})
		shell, err := tifa.Use(c, Layout)
		if err != nil {
			t.Fatal(err)
		}
		if shell.SidebarOpen != tc.open {
			t.Errorf("cookie %q: sidebar open %v, want %v", tc.cookie, shell.SidebarOpen, tc.open)
		}
		if len(shell.Projects) != 2 {
			t.Errorf("cookie %q: projects %+v", tc.cookie, shell.Projects)
		}
	}
}

// The Layout runs for every URL, the 404 page included, so a visitor who is not signed in gets no
// projects from it. In the open workbench of the comparison everyone gets them.
func TestLayoutHasNoProjectsForAVisitorWhoIsNotSignedIn(t *testing.T) {
	r := httptest.NewRequest("GET", "/nope", nil)
	anonymous, _ := tifatest.NewCtx(t, r)
	shell, err := tifa.Use(anonymous, Layout)
	if err != nil {
		t.Fatal(err)
	}
	if shell.Projects == nil || len(shell.Projects) != 0 {
		t.Errorf("signed out: projects %#v, want an empty list", shell.Projects)
	}
	shop.Open = true
	t.Cleanup(func() { shop.Open = false })
	open, _ := tifatest.NewCtx(t, r)
	if shell, err := tifa.Use(open, Layout); err != nil || len(shell.Projects) != 2 {
		t.Errorf("open workbench: %+v %v", shell, err)
	}
}

// The Layout is the same for every signed-in user, so a page below it can be cached. The
// signed-in user reaches the page through useSession, not through the data of the Layout.
func TestLayoutDataIsTheSameForEverySignedInUser(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio", nil)
	first, _ := tifatest.NewCtx(t, r)
	tifatest.SignIn(t, first, &shop.Session{UserID: "u-1", Name: "顾遥", Initials: "顾"})
	want, err := tifa.Use(first, Layout)
	if err != nil {
		t.Fatal(err)
	}
	signedIn, _ := tifatest.NewCtx(t, r)
	tifatest.SignIn(t, signedIn, &shop.Session{UserID: "u-9", Name: "周宁", Initials: "周"})
	got, err := tifa.Use(signedIn, Layout)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("one user: %+v, another: %+v", got, want)
	}
	if data, _ := json.Marshal(got); strings.Contains(string(data), "周宁") {
		t.Errorf("the Layout data holds the user: %s", data)
	}
}
