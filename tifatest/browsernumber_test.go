package tifatest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

type bigID struct {
	ID int64 `json:"id"`
}

// A test pins that a handler's result reaches the browser whole by calling it as the generated
// function of a page does: with the header X-Tifa-Call. A plain request is answered as written.
func TestHandlerCallAsThePageDoesRefusesARoundedNumber(t *testing.T) {
	app := server.App{Routes: []server.Route{
		{ID: "app/api/make/route", Path: "/api/make",
			POST: server.Output(func(c *tifa.Ctx) (bigID, error) { return bigID{ID: 1<<53 + 1}, nil })},
	}}
	srv := tifatest.Start(t, app)
	post := func(call bool) (int, string) {
		req, err := http.NewRequest("POST", srv.URL+"/api/make", nil)
		if err != nil {
			t.Fatal(err)
		}
		if call {
			req.Header.Set("X-Tifa-Call", "1")
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	if code, body := post(false); code != 200 || !strings.Contains(body, "9007199254740993") {
		t.Errorf("a plain request: %d %s", code, body)
	}
	if code, body := post(true); code != 500 {
		t.Errorf("a call as the page does: %d %s, want 500", code, body)
	}
}

// Under Start a page answers its Load data as JSON, and a number a browser would round is refused
// as it is for a page, so a test sees what the program does.
func TestStartRefusesARoundedNumberInLoadData(t *testing.T) {
	app := server.App{Routes: []server.Route{
		{ID: "app/big/route", Path: "/big", Page: true, SSR: true,
			Load: func(c *tifa.Ctx) (any, error) { return bigID{ID: 1<<53 + 1}, nil }},
		{ID: "app/ok/route", Path: "/ok", Page: true, SSR: true,
			Load: func(c *tifa.Ctx) (any, error) { return bigID{ID: 1<<53 - 1}, nil }},
	}}
	srv := tifatest.Start(t, app)
	get := func(path string) (int, string) {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	if code, body := get("/ok"); code != 200 || !strings.Contains(body, "9007199254740991") {
		t.Errorf("a safe number: %d %s", code, body)
	}
	if code, body := get("/big"); code != 500 {
		t.Errorf("a rounded number: %d %s, want 500", code, body)
	}
}
