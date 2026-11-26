package server

import (
	"errors"
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
	for _, target := range []string{"/list?page=2.5", "/list?page=1&page=2", "/list?page=x"} {
		r := routesOf(t, getData(s, target, "b1", "1"))["app/list/route"]
		if r.Status != 400 {
			t.Errorf("%s: %+v, want 400", target, r)
		}
	}
	if w := get(s, "/list?page=2.5"); w.Code != 400 {
		t.Errorf("the page for a value that is not valid: %d", w.Code)
	}
	if w := get(s, "/list?page=3&utm_source=x"); w.Code != 200 || !strings.Contains(w.Body.String(), "page 3") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}

type checkedListQuery struct {
	Page int `json:"page"`
}

func (q checkedListQuery) Validate() error {
	switch {
	case q.Page < 0:
		return tifa.Error{Status: 400, Message: "page must be at least 0"}
	case q.Page > 99:
		return errors.New("the list has no such page")
	}
	return nil
}

func checkedListLoad(c *tifa.Ctx, q checkedListQuery) (string, error) {
	return "page " + strconv.Itoa(q.Page), nil
}

// The Validate method of a query struct runs before the Load, and its error is the error of the
// Load: a tifa.Error as it is, any other error through OnError.
func TestValidateOfAQueryStructIsTheErrorOfTheLoad(t *testing.T) {
	var reported []error
	app := App{Routes: []Route{{
		ID: "app/list/route", Path: "/list", Page: true, SSR: true, Layouts: []string{},
		Load: LoadInput(checkedListLoad), Query: []string{"page"},
	}}}
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		reported = append(reported, err)
		return tifa.Error{Status: 500, Message: "something went wrong"}
	}
	s, _ := newFrontServer(t, app)
	if r := routesOf(t, getData(s, "/list?page=-1", "b1", "1"))["app/list/route"]; r.Status != 400 {
		t.Errorf("a value Validate refuses: %+v", r)
	}
	if w := get(s, "/list?page=-1"); w.Code != 400 || !strings.Contains(w.Body.String(), "page must be at least 0") {
		t.Errorf("the page: %d %s", w.Code, w.Body.String())
	}
	if len(reported) != 0 {
		t.Errorf("a tifa.Error went to OnError: %v", reported)
	}
	if w := get(s, "/list?page=100"); w.Code != 500 || len(reported) != 1 || reported[0].Error() != "the list has no such page" {
		t.Errorf("a plain error: %d, reported %v", w.Code, reported)
	}
	if w := get(s, "/list?page=7"); w.Code != 200 || !strings.Contains(w.Body.String(), "page 7") {
		t.Errorf("a valid value: %d %s", w.Code, w.Body.String())
	}
}
