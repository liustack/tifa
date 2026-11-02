package server

import (
	"net/http/httptest"
	"testing"
)

// A middleware set with tifa.SetHTTPHandler reads the route template from the request it passed
// on, once the handler has returned, to count requests by route.
func TestRequestPatternIsTheRouteTemplate(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	for target, want := range map[string]string{
		"/projects/p1":     "/projects/{projectId}",
		"/projects/p1?x=1": "/projects/{projectId}",
		"/login":           "/login",
		"/api/health":      "/api/health",
		"/nothing/here":    "",
		"/":                "/",
	} {
		r := httptest.NewRequest("GET", target, nil)
		s.ServeHTTP(httptest.NewRecorder(), r)
		if r.Pattern != want {
			t.Errorf("%s: Pattern %q, want %q", target, r.Pattern, want)
		}
	}
}
