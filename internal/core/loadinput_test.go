package core

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type pageFilter struct {
	Page int    `json:"page"`
	Sort string `json:"sort"`
}

var loadRuns, otherRuns atomic.Int32

func pageLoad(c *Ctx, q pageFilter) (string, error) {
	loadRuns.Add(1)
	return q.Sort + strings.Repeat("!", q.Page), nil
}

func otherLoad(c *Ctx, q pageFilter) (string, error) {
	otherRuns.Add(1)
	return "other", nil
}

func newQueryCtx(target string) *Ctx {
	return New(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil), "/list", 1024)
}

// The wrapper of a Load with a query struct decodes the query string, and runs once per request
// like a Load that goes through Use. Two Loads of one signature are two Loads: the key is the
// function the app wrote, not the wrapper.
func TestLoadInputDecodesAndRunsOncePerRequest(t *testing.T) {
	load, other := LoadInput(pageLoad), LoadInput(otherLoad)
	loadRuns.Store(0)
	otherRuns.Store(0)
	c := newQueryCtx("/list?page=2&sort=name")
	for range 3 {
		v, err := load(c)
		if err != nil || v != "name!!" {
			t.Fatalf("got %v, %v", v, err)
		}
	}
	if v, err := other(c); err != nil || v != "other" {
		t.Fatalf("the second Load got %v, %v", v, err)
	}
	if loadRuns.Load() != 1 || otherRuns.Load() != 1 {
		t.Errorf("ran %d and %d times, want once each", loadRuns.Load(), otherRuns.Load())
	}
}

func TestLoadInputAnswers400ForAValueThatIsNotValid(t *testing.T) {
	before := loadRuns.Load()
	var e Error
	_, err := LoadInput(pageLoad)(newQueryCtx("/list?page=two"))
	if !errors.As(err, &e) || e.Status != 400 || e.Message != `invalid query parameter "page"` {
		t.Fatalf("got %v", err)
	}
	if loadRuns.Load() != before {
		t.Error("the Load ran for a query string that does not decode")
	}
}

// A Load that runs through Use is in Load mode: it gets its query parameters as the second
// parameter and a read of the request's query string there is a mistake of the program.
func TestQueryReadInALoadPanicsWithTheFix(t *testing.T) {
	for name, read := range map[string]func(*Ctx){
		"Query":       func(c *Ctx) { c.Query("sort") },
		"QueryValues": func(c *Ctx) { c.QueryValues("sort") },
		"BindQuery": func(c *Ctx) {
			var v struct {
				Sort string `json:"sort"`
			}
			c.BindQuery(&v)
		},
	} {
		c := newQueryCtx("/list?sort=name")
		_, err := Use(c, func(c *Ctx) (int, error) { read(c); return 0, nil })
		if err == nil || !strings.Contains(err.Error(), "tifa: c."+name) || !strings.Contains(err.Error(), "second parameter") {
			t.Errorf("c.%s in a Load: got %v", name, err)
		}
		// Outside Load, in a handler or OnRequest, it reads as before.
		var v struct {
			Sort string `json:"sort"`
		}
		if c.Query("sort") != "name" || len(c.QueryValues("sort")) != 1 || c.BindQuery(&v) != nil || v.Sort != "name" {
			t.Errorf("c.%s outside Load does not read the query string", name)
		}
	}
}
