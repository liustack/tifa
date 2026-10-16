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
		Layouts: []Layout{{ID: "app/layout", SSR: true, Load: func(c *tifa.Ctx) (any, error) {
			if c.Query("deny") != "" {
				return nil, errDenied
			}
			return "root", nil
		}}},
		Routes: []Route{{ID: "app/secret", Path: "/secret", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return "PRIVATE_SECRET", nil }}},
	}
}

var secretData = []string{"X-Tifa-Route", "app/layout,app/secret", "X-Tifa-Build", "b1"}

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
	if w.Code != 200 || got["app/layout"].Status != 500 || got["app/layout"].Error != "reported" || got["app/secret"].Status != 200 {
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

// A redirect answers a data request with a redirect the collector follows, not with 3xx,
// which fetch would follow with the headers of the old page.
func TestDataRequestRedirectIsJSON(t *testing.T) {
	var reported []error
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx) error {
		if _, err := c.Cookie("session"); err != nil {
			return c.Redirect("/login", 302)
		}
		return c.Next()
	}, &reported))
	w := get(s, "/secret", secretData...)
	if w.Code != 200 || w.Header().Get("Location") != "" || strings.TrimSpace(w.Body.String()) != `{"redirect":"/login","status":302}` {
		t.Errorf("data request: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w := get(s, "/secret"); w.Code != 302 || w.Header().Get("Location") != "/login" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}
	// ServeMux redirects a non-canonical path the same way.
	if w := get(s, "/./secret", append([]string{"Cookie", "session=1"}, secretData...)...); w.Code != 200 || !strings.Contains(w.Body.String(), `"redirect":"/secret"`) {
		t.Errorf("non-canonical path: %d %v %s", w.Code, w.Header(), w.Body.String())
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

// A relative redirect resolves against the URL of the request, as http.Redirect resolves it for
// a page, before it goes into the JSON of a data request. The router would resolve it against
// something else.
func TestDataRequestResolvesARelativeRedirect(t *testing.T) {
	var reported []error
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx) error {
		if c.Query("away") != "" {
			return c.Redirect("login?next=1", 302)
		}
		return c.Next()
	}, &reported))
	w := get(s, "/secret?away=1", secretData...)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"redirect":"/login?next=1","status":302}` {
		t.Errorf("OnRequest: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/secret?away=1"); w.Code != 302 || w.Header().Get("Location") != "/login?next=1" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}

	app := App{
		Layouts: []Layout{{ID: "app/layout", SSR: true}},
		Routes: []Route{{ID: "app/projects/orders", Path: "/projects/{project}/orders", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return nil, c.Redirect("../other/orders", 303) }}},
	}
	s, _ = newFrontServer(t, app)
	w = get(s, "/projects/p1/orders", "X-Tifa-Route", "app/projects/orders", "X-Tifa-Build", "b1")
	var got map[string]routeResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if r := got["app/projects/orders"]; r.Status != 303 || r.Location != "/projects/other/orders" {
		t.Errorf("Load: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p1/orders"); w.Code != 303 || w.Header().Get("Location") != "/projects/other/orders" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}
}
