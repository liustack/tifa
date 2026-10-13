package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func testApp(name string) string {
	return filepath.Join("..", "..", "internal", "analyze", "testdata", name)
}

func TestRoutesPrintsTable(t *testing.T) {
	out, _, err := run(t, "routes", "--dir", testApp("valid"))
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	for _, line := range [][]string{
		{"ROUTE", "PATH", "LAYOUTS", "METHODS", "SSR", "CACHE"},
		{"app/projects/orders/detail", "/projects/{projectId}/orders/{orderId}", "app > app/projects", "page POST DELETE", "false", "false"},
		{"app/projects/list", "/projects/{projectId}", "app > app/projects", "page", "false", "true"},
		{"app/api/health", "/api/health", "app", "GET", "true", "false"},
		{"LAYOUT", "PATH", "PARENT", "SSR"},
		{"app/projects", "/projects/{projectId}", "app", "false"},
	} {
		if !hasLine(out, line) {
			t.Errorf("no line with fields %q in:\n%s", line, out)
		}
	}
}

// hasLine reports whether some line of out holds want as consecutive whitespace-separated fields.
func hasLine(out string, want []string) bool {
	joined := strings.Join(strings.Fields(strings.Join(want, " ")), " ")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(" "+strings.Join(strings.Fields(line), " ")+" ", " "+joined+" ") {
			return true
		}
	}
	return false
}

func TestRoutesJSON(t *testing.T) {
	out, _, err := run(t, "routes", "--json", "--dir", testApp("valid"))
	if err != nil {
		t.Fatalf("routes --json: %v", err)
	}
	var m struct {
		Routes []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(m.Routes) != 8 {
		t.Errorf("got %d routes", len(m.Routes))
	}
}

func TestRoutesReportsEveryViolation(t *testing.T) {
	_, stderr, err := run(t, "routes", "--dir", testApp("useclosure"))
	if err == nil {
		t.Fatal("routes succeeded on an app with violations")
	}
	for _, want := range []string{"app/page/route.go:12:", "app/page/route.go:13:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}
