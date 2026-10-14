package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

var errDenied = errors.New("permission denied")

// secretApp has a root Layout whose Load refuses ?deny=1 and a page whose own Load does not
// depend on it and returns sensitive data.
func secretApp(onRequest func(c *tifa.Ctx) error, onError *[]error) App {
	return App{
		Hooks: Hooks{
			OnRequest: onRequest,
			OnError: func(c *tifa.Ctx, err error) tifa.Error {
				*onError = append(*onError, err)
				return tifa.Error{Status: 500, Message: "reported"}
			},
		},
		Layouts: []Layout{{ID: "app", SSR: true, Load: func(c *tifa.Ctx) (any, error) {
			if c.Query("deny") != "" {
				return nil, errDenied
			}
			return "root", nil
		}}},
		Routes: []Route{{ID: "app/secret", Path: "/secret", Page: true, SSR: true, Layouts: []string{"app"},
			Load: func(c *tifa.Ctx) (any, error) { return "PRIVATE_SECRET", nil }}},
	}
}

var secretData = []string{"X-Tifa-Route", "app,app/secret", "X-Tifa-Build", "b1"}

// A middleware that refuses any request whose Load failed refuses the data request as it
// refuses the page.
func TestDataRequestFailsNextLikeThePage(t *testing.T) {
	var reported []error
	var seen []error
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx) error {
		err := c.Next()
		seen = append(seen, err)
		if err != nil {
			return tifa.Error{Status: 403, Message: "denied"}
		}
		return nil
	}, &reported))
	page := get(s, "/secret?deny=1")
	data := get(s, "/secret?deny=1", secretData...)
	for name, w := range map[string]int{"page": page.Code, "data": data.Code} {
		if w != 403 {
			t.Errorf("%s: %d", name, w)
		}
	}
	if strings.Contains(data.Body.String(), "PRIVATE_SECRET") || !strings.Contains(data.Body.String(), "denied") {
		t.Errorf("data body %s", data.Body.String())
	}
	if len(seen) != 2 || !errors.Is(seen[0], errDenied) || !errors.Is(seen[1], errDenied) {
		t.Errorf("Next returned %v", seen)
	}
	// OnError only sees the errors OnRequest leaves unhandled.
	if len(reported) != 0 {
		t.Errorf("OnError got %v", reported)
	}
}

// When OnRequest returns the error of Next as it is, the data request is answered by route,
// each with its own status, and OnError runs once, after OnRequest.
func TestDataRequestAnswersByRouteWhenNextFailsUnhandled(t *testing.T) {
	var reported []error
	var order []string
	app := secretApp(func(c *tifa.Ctx) error {
		err := c.Next()
		order = append(order, "OnRequest returns")
		return err
	}, &reported)
	onError := app.Hooks.OnError
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		order = append(order, "OnError")
		return onError(c, err)
	}
	s, _ := newFrontServer(t, app)
	w := get(s, "/secret?deny=1", secretData...)
	var got map[string]routeResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w.Code != 200 || got["app"].Status != 500 || got["app"].Error != "reported" || got["app/secret"].Status != 200 {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	if strings.Join(order, ", ") != "OnRequest returns, OnError" || len(reported) != 1 || !errors.Is(reported[0], errDenied) {
		t.Errorf("order %v, OnError got %v", order, reported)
	}
	order = nil
	if w := get(s, "/secret?deny=1"); w.Code != 500 || strings.Join(order, ", ") != "OnRequest returns, OnError" {
		t.Errorf("page: %d, order %v", w.Code, order)
	}
}

// A failed render of an error page is an unexpected error of its own: OnError sees it and Go
// writes the page.
func TestFailedErrorPageGoesToOnError(t *testing.T) {
	var reported []error
	app := secretApp(nil, &reported)
	s, r := newFrontServer(t, app)
	r.result = func(ssr.Input) (string, error) { return "", errors.New("render timed out") }
	w := get(s, "/nowhere")
	if w.Code != 500 || !strings.Contains(w.Body.String(), "<h1>500 Internal Server Error</h1>") || !strings.Contains(w.Body.String(), "reported") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "render timed out") {
		t.Errorf("OnError got %v", reported)
	}
	if r.count() != 1 {
		t.Errorf("%d renders", r.count())
	}
}
