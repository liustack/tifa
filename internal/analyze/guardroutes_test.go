package analyze

import (
	"slices"
	"strings"
	"testing"
)

// A string that c.Pattern() is compared with and that no route of the app can have is reported,
// since the guard silently stops matching after a folder is renamed. The patterns are written as
// c.Pattern() returns them: no method, {name} and {name...} for parameters.
func TestWarnsOfGuardStringsThatMatchNoRoute(t *testing.T) {
	m, err := analyzeApp(t, "guardroutes")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := []struct{ at, lit, near string }{
		{"hooks.go:10:", `"/photo"`, "/photos"},
		{"hooks.go:13:", `"/account/orders/{id}"`, "/account/orders/{order}"},
		{"hooks.go:16:", `"/acount/"`, "/account"},
		{"hooks.go:19:", `"GET /photos"`, "/photos"},
		{"hooks.go:22:", `"/acount"`, "/account"},
		{"hooks.go:22:", `"/nothing/"`, "never matches"},
		{"hooks.go:26:", `"/photoz"`, "/photos"},
		{"hooks.go:28:", `"/files/{path}"`, "/files/{path...}"},
	}
	var got []string
	for _, w := range m.Warnings {
		got = append(got, w.String())
	}
	if len(got) != len(want) {
		t.Fatalf("got %d warnings, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i, w := range want {
		for _, part := range []string{w.at, w.lit, w.near, "never matches"} {
			if !strings.Contains(got[i], part) {
				t.Errorf("warning %d lacks %q:\n%s", i, part, got[i])
			}
		}
	}
	if !strings.Contains(got[0], "hooks.go:10:") || !strings.Contains(got[0], "no route of the app") {
		t.Errorf("unexpected wording:\n%s", got[0])
	}
}

// What can match stays quiet: routes that exist, a prefix of a route, the built-in files, the
// empty pattern of an unmatched URL, and everything the build cannot read from the code (a
// variable, a changed local, a Pattern method of another type).
func TestGuardStringsThatCanMatchStayQuiet(t *testing.T) {
	m, err := analyzeApp(t, "guardquiet")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !slices.Equal(m.Warnings, nil) {
		t.Fatalf("warnings:\n%v", m.Warnings)
	}
}
