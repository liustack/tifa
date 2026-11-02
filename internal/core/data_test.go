package core

import (
	"net/http/httptest"
	"testing"
)

func TestSplitData(t *testing.T) {
	cases := []struct {
		target  string
		query   string
		build   string
		layers  string
		preload bool
		valid   bool
	}{
		{"/a?_tifa=b1.101", "", "b1", "101", false, true},
		{"/a?x=1&_tifa=b1.1.p&y=a+b", "x=1&y=a+b", "b1", "1", true, true},
		{"/a?%5Ftifa=b1.01", "", "b1", "01", false, true},
		{"/a?_tifa=1.2.3.0110", "", "1.2.3", "0110", false, true},
		{"/a?_tifa=b1", "", "", "", false, false},
		{"/a?_tifa=b1.12", "", "b1", "12", false, false},
		{"/a?_tifa=", "", "", "", false, false},
		{"/a?_tifa=%zz", "", "", "", false, false},
		{"/a?_%74ifa=b1.01&x=1", "x=1", "b1", "01", false, true},
		{"/a?%5F%74%69%66%61=b1.01.p", "", "b1", "01", true, true},
		{"/a?x=1&%5F%74ifa=b1.1&y=2", "x=1&y=2", "b1", "1", false, true},
		{"/a?_tifa=b1.1&_tifa=b2.0", "", "b1", "1", false, true},
	}
	for _, tc := range cases {
		orig := httptest.NewRequest("GET", tc.target, nil)
		before := orig.URL.RawQuery
		r := SplitData(orig)
		d := DataOf(r)
		if d == nil || r.URL.RawQuery != tc.query || d.Build != tc.build || d.Layers != tc.layers || d.Preload != tc.preload || d.Valid != tc.valid || d.Original != orig {
			t.Errorf("%s: query %q, %+v", tc.target, r.URL.RawQuery, d)
		}
		if orig.URL.RawQuery != before || DataOf(orig) != nil {
			t.Errorf("%s: the original request changed", tc.target)
		}
		if r.RequestURI != r.URL.RequestURI() {
			t.Errorf("%s: RequestURI %q", tc.target, r.RequestURI)
		}
	}
	for _, target := range []string{"/a", "/a?x=1", "/a?tifa=1", "/a?x_tifa=1", "/a?%5F%54ifa=1"} {
		r := httptest.NewRequest("GET", target, nil)
		if SplitData(r) != r || DataOf(r) != nil {
			t.Errorf("%s: a request without the parameter was changed", target)
		}
	}
}
