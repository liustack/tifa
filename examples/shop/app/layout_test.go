package app

import (
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
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

// The storefront is for customers, so its pages carry none of the workbench: not the shop
// owner, not the shops, not their orders waiting to ship.
func TestLayoutLeavesTheWorkbenchOutOfTheStorefront(t *testing.T) {
	for _, pattern := range []string{"/store/{lang}", "/store/{lang}/cart", "/store/{lang}/products/{product}"} {
		r := httptest.NewRequest("GET", "/store/zh-CN", nil)
		r.Pattern = pattern
		c, _ := tifatest.NewCtx(t, r)
		shell, err := tifa.Use(c, Layout)
		if err != nil {
			t.Fatal(err)
		}
		if shell.Viewer != (Viewer{}) || shell.Projects != nil || shell.SidebarOpen {
			t.Errorf("%s: %+v", pattern, shell)
		}
	}
	r := httptest.NewRequest("GET", "/projects/studio", nil)
	r.Pattern = "/projects/{project}"
	c, _ := tifatest.NewCtx(t, r)
	if shell, err := tifa.Use(c, Layout); err != nil || shell.Viewer.Name == "" || len(shell.Projects) != 2 {
		t.Errorf("workbench: %+v %v", shell, err)
	}
}
