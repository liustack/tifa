package server

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

var errDenied = errors.New("permission denied")

// secretApp has a root Layout whose Load refuses ?deny=1 and a page whose own Load does not
// depend on it and returns sensitive data.
func secretApp(onRequest func(c *tifa.Ctx, next func(*tifa.Ctx) error) error, onError *[]error) App {
	return App{
		Hooks: Hooks{
			OnRequest: onRequest,
			OnError: func(c *tifa.Ctx, err error) tifa.Error {
				*onError = append(*onError, err)
				return tifa.Error{Status: 500, Message: "reported"}
			},
		},
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true, Load: func(c *tifa.Ctx) (any, error) {
			if c.Query("deny") != "" {
				return nil, errDenied
			}
			return "root", nil
		}}},
		Routes: []Route{{ID: "app/secret", Path: "/secret", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return "PRIVATE_SECRET", nil }}},
	}
}

func getSecret(s *server, target string, header ...string) *httptest.ResponseRecorder {
	return getData(s, target, "b1", "11", header...)
}

// A middleware that refuses any request whose Load failed refuses the data request as it
// refuses the page.
func TestDataRequestFailsNextLikeThePage(t *testing.T) {
	var reported []error
	var seen []error
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		err := next(c)
		seen = append(seen, err)
		if err != nil {
			return tifa.Error{Status: 403, Message: "denied"}
		}
		return nil
	}, &reported))
	page := get(s, "/secret?deny=1")
	data := getSecret(s, "/secret?deny=1")
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
	app := secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		err := next(c)
		order = append(order, "OnRequest returns")
		return err
	}, &reported)
	onError := app.Hooks.OnError
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		order = append(order, "OnError")
		return onError(c, err)
	}
	s, _ := newFrontServer(t, app)
	w := getSecret(s, "/secret?deny=1")
	got := routesOf(t, w)
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
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if _, err := c.Cookie("session"); err != nil {
			return tifa.Redirect("/login", 302)
		}
		return next(c)
	}, &reported))
	w := getSecret(s, "/secret")
	if w.Code != 200 || w.Header().Get("Location") != "" || strings.TrimSpace(w.Body.String()) != `{"redirect":"/login","status":302}` {
		t.Errorf("data request: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w := get(s, "/secret"); w.Code != 302 || w.Header().Get("Location") != "/login" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}
	// ServeMux redirects a non-canonical path the same way.
	if w := getSecret(s, "/./secret", "Cookie", "session=1"); w.Code != 200 || !strings.Contains(w.Body.String(), `"redirect":"/secret"`) {
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
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Query("away") != "" {
			return tifa.Redirect("login?next=1", 302)
		}
		return next(c)
	}, &reported))
	w := getSecret(s, "/secret?away=1")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"redirect":"/login?next=1","status":302}` {
		t.Errorf("OnRequest: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/secret?away=1"); w.Code != 302 || w.Header().Get("Location") != "/login?next=1" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}

	app := App{
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true}},
		Routes: []Route{{ID: "app/projects/orders", Path: "/projects/{project}/orders", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.Redirect("../other/orders", 303) }}},
	}
	s, _ = newFrontServer(t, app)
	w = getData(s, "/projects/p1/orders", "b1", "1")
	got := routesOf(t, w)
	if r := got["app/projects/orders"]; r.Status != 303 || r.Location != "/projects/other/orders" {
		t.Errorf("Load: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p1/orders"); w.Code != 303 || w.Header().Get("Location") != "/projects/other/orders" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}
}

// A handler call of the browser that OnRequest redirects gets the JSON redirect on a server
// with pages too, not the 3xx a page request gets.
func TestHandlerCallRedirectOfOnRequestIsJSON(t *testing.T) {
	var reported []error
	s, _ := newFrontServer(t, secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if _, err := c.Cookie("session"); err != nil {
			return tifa.Redirect("/login", 303)
		}
		return next(c)
	}, &reported))
	r := httptest.NewRequest("POST", "/secret", strings.NewReader("{}"))
	r.Header.Set("X-Tifa-Call", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("X-Tifa-Redirect") != "303" || strings.TrimSpace(w.Body.String()) != `{"redirect":"/login","status":303}` {
		t.Errorf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

// A path ServeMux cleans (a double slash, a dot segment) is another spelling of the same page.
// Search engines take only a permanent redirect as a sign of the canonical URL, and 308 keeps the
// method, where the 301 of ServeMux turns a POST into a GET.
func TestPathCleaningRedirectIsPermanent(t *testing.T) {
	var reported []error
	s, _ := newFrontServer(t, secretApp(nil, &reported))
	for _, tc := range []struct{ target, want string }{
		{"/./secret", "/secret"},
		{"/a/../secret?x=1", "/secret?x=1"},
		{"//secret", "/secret"},
	} {
		w := get(s, tc.target)
		if w.Code != 308 || w.Header().Get("Location") != tc.want {
			t.Errorf("%s: %d %v", tc.target, w.Code, w.Header())
		}
	}
	r := httptest.NewRequest("POST", "/api//health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 308 {
		t.Errorf("POST: %d %v", w.Code, w.Header())
	}
}
