package shop

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

func TestOnRequestRefusesWritesFromOtherSites(t *testing.T) {
	routeErr := errors.New("route failed")
	cases := []struct {
		method, origin string
		ran            bool
	}{
		{"GET", "", true},
		{"POST", "http://example.com", true},
		{"POST", "https://evil.test", false},
		{"POST", "", false},
	}
	for _, tc := range cases {
		// The route answers with nil or with an error, and OnRequest passes on either.
		for _, want := range []error{nil, routeErr} {
			r := httptest.NewRequest(tc.method, "http://example.com/projects/studio/orders/SH-1041", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			c, _ := tifatest.NewCtx(t, r)
			ran := false
			tifatest.SetNext(c, func() error { ran = true; return want })
			err := OnRequest(c)
			var e tifa.Error
			switch {
			case ran != tc.ran:
				t.Errorf("%s from %q: route ran %v", tc.method, tc.origin, ran)
			case tc.ran && err != want:
				t.Errorf("%s from %q: got %v, want %v from the route", tc.method, tc.origin, err, want)
			case !tc.ran && (!errors.As(err, &e) || e.Status != 403):
				t.Errorf("%s from %q: got %v, want 403", tc.method, tc.origin, err)
			}
		}
	}
}
