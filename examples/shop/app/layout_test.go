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

// The page of an order is cached, so the Layout above it holds what is the same for everyone. The
// signed-in user reaches the page through useSession, not through the data of the Layout.
func TestLayoutDataIsTheSameForEveryone(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio", nil)
	anonymous, _ := tifatest.NewCtx(t, r)
	want, err := tifa.Use(anonymous, Layout)
	if err != nil {
		t.Fatal(err)
	}
	signedIn, _ := tifatest.NewCtx(t, r)
	tifatest.WithSession(signedIn, &shop.Session{UserID: "u-9", Name: "周宁", Initials: "周"})
	got, err := tifa.Use(signedIn, Layout)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("signed in: %+v, signed out: %+v", got, want)
	}
	if data, _ := json.Marshal(got); strings.Contains(string(data), "周宁") {
		t.Errorf("the Layout data holds the user: %s", data)
	}
}
