package server

import (
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

type listQuery struct {
	Page int `json:"page"`
}

func listLoad(c *tifa.Ctx, q listQuery) (string, error) { return "page " + strconv.Itoa(q.Page), nil }

// A page whose Load takes its query parameters as a second parameter gets them decoded, and a
// value that is not valid is the 400 of that Load.
func TestLoadWithQueryInputOnAPage(t *testing.T) {
	app := App{Routes: []Route{{
		ID: "app/list/route", Path: "/list", Page: true, SSR: true, Layouts: []string{},
		Load: LoadInput(listLoad), Query: []string{"page"},
	}}}
	s, _ := newFrontServer(t, app)
	routes := routesOf(t, getData(s, "/list?page=2", "b1", "1"))
	if r := routes["app/list/route"]; r.Status != 200 {
		t.Errorf("a valid value: %+v", r)
	}
	for _, target := range []string{"/list?page=02", "/list?page=1&page=2", "/list?page=x"} {
		r := routesOf(t, getData(s, target, "b1", "1"))["app/list/route"]
		if r.Status != 400 {
			t.Errorf("%s: %+v, want 400", target, r)
		}
	}
	if w := get(s, "/list?page=02"); w.Code != 400 {
		t.Errorf("the page for a value that is not valid: %d", w.Code)
	}
	if w := get(s, "/list?page=3&utm_source=x"); w.Code != 200 || !strings.Contains(w.Body.String(), "page 3") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}
