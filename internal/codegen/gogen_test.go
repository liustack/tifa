package codegen

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
)

// app.go imports tifa only for what refers to it: a Load or Layout without a query struct goes
// through tifa.Use, and a handler with an input names (*tifa.Ctx).Bind. A Load with a query struct
// needs no import of it, and an import that nothing uses stops the program from compiling.
func TestGeneratedAppImportsTifaOnlyWhenItUsesIt(t *testing.T) {
	handler := analyze.Route{ID: "app/a/route", Path: "/a", Package: "example.com/x/app/a", Dir: "app/a",
		Handlers: []analyze.Handler{{Method: "POST", Input: "example.com/x/app/a.In"}, {Method: "GET", Input: "example.com/x/app/a.Q", Output: "string"}}}
	queryLoad := analyze.Route{ID: "app/b/route", Path: "/b", Page: true, Package: "example.com/x/app/b", Dir: "app/b",
		Load: &analyze.Load{Input: "example.com/x/app/b.Filter", Query: []string{"page"}}}
	plainLoad := analyze.Route{ID: "app/c/route", Path: "/c", Page: true, Package: "example.com/x/app/c", Dir: "app/c", Load: &analyze.Load{}}
	const importTifa = "\t\"github.com/liustack/tifa\"\n"
	for name, tc := range map[string]struct {
		routes []analyze.Route
		want   bool
	}{
		"handlers with inputs":         {[]analyze.Route{handler}, true},
		"a Load with a query struct":   {[]analyze.Route{queryLoad}, false},
		"a Load that goes through Use": {[]analyze.Route{plainLoad}, true},
	} {
		m := &analyze.Manifest{Module: "example.com/x", Routes: tc.routes}
		src, err := appSource(m, nil, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Contains(string(src), importTifa); got != tc.want {
			t.Errorf("%s: imports tifa %v, want %v\n%s", name, got, tc.want, src)
		}
	}
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{queryLoad}}
	src, _ := appSource(m, nil, "")
	if !strings.Contains(string(src), "Load:    server.LoadInput(app_b.Load),") {
		t.Errorf("a Load with a query struct is wrapped by server.LoadInput:\n%s", src)
	}
}
