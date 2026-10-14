package devserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fakePipeline records the steps it runs. fingerprint is what Analyze returns, fail names a step that fails.
type fakePipeline struct {
	calls       []string
	fingerprint string
	fail        string
	detail      string // what Bundle adds to its log line
}

func (p *fakePipeline) step(name string) error {
	p.calls = append(p.calls, name)
	if p.fail == name {
		return errors.New(name + " broke")
	}
	return nil
}

func (p *fakePipeline) Analyze(context.Context) (string, error) {
	return p.fingerprint, p.step("Analyze")
}
func (p *fakePipeline) Generate(context.Context) error { return p.step("Generate") }
func (p *fakePipeline) Bundle(context.Context) (string, error) {
	return p.detail, p.step("Bundle")
}
func (p *fakePipeline) GoBuild(context.Context) error { return p.step("GoBuild") }

var all = []string{"Analyze", "Generate", "Bundle", "GoBuild"}

// built returns a builder that already finished the first full build with fingerprint "v1".
func built(t *testing.T) (*builder, *fakePipeline) {
	t.Helper()
	p := &fakePipeline{fingerprint: "v1"}
	b := &builder{p: p, log: t.Logf}
	if _, err := b.build(context.Background(), everything, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.calls, all) {
		t.Fatalf("first build ran %v, want %v", p.calls, all)
	}
	p.calls = nil
	return b, p
}

func TestRebuildMatrix(t *testing.T) {
	cases := []struct {
		name        string
		changes     []change
		fingerprint string
		want        []string
	}{
		{"tsx", []change{{"app/orders/list/route.tsx", modified}}, "v1", []string{"Bundle"}},
		{"ts", []change{{"app/orders/format.ts", modified}}, "v1", []string{"Bundle"}},
		{"css", []change{{"app/app.css", created}}, "v1", []string{"Bundle"}},
		{"new component", []change{{"app/orders/list/items.tsx", created}}, "v1", []string{"Bundle"}},
		{"go body", []change{{"app/orders/store.go", modified}}, "v1", []string{"Analyze", "GoBuild"}},
		{"go.mod", []change{{"go.mod", modified}}, "v1", []string{"Analyze", "GoBuild"}},
		{"go routes", []change{{"app/orders/list/route.go", modified}}, "v2", all},
		{"new route", []change{{"app/orders/new/route.tsx", created}}, "v1", all},
		{"removed route", []change{{"app/orders/new/route.tsx", removed}}, "v1", all},
		{"removed layout", []change{{"app/orders/layout.tsx", removed}}, "v1", all},
		{"config", []change{{"tifa.yaml", modified}}, "v1", all},
		{"env", []change{{".env", modified}}, "v1", []string{"Bundle"}},
		{"env of a mode", []change{{".env.production.local", created}}, "v1", []string{"Bundle"}},
		{"go and tsx", []change{{"app/orders/store.go", modified}, {"app/app.css", modified}}, "v1", []string{"Analyze", "Bundle", "GoBuild"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p := built(t)
			p.fingerprint = tc.fingerprint
			ran, err := b.build(context.Background(), plan(tc.changes), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.calls, tc.want) {
				t.Fatalf("ran %v, want %v", p.calls, tc.want)
			}
			// The steps build returns are the ones it ran.
			want := steps{
				analyze:  slices.Contains(tc.want, "Analyze"),
				generate: slices.Contains(tc.want, "Generate"),
				bundle:   slices.Contains(tc.want, "Bundle"),
				goBuild:  slices.Contains(tc.want, "GoBuild"),
			}
			if ran != want {
				t.Fatalf("build returned %+v, want %+v", ran, want)
			}
		})
	}
}

func TestPlanIgnoresOtherFiles(t *testing.T) {
	for _, path := range []string{"README.md", "app/logo.png", "app/tifa.yaml", "app/notes.txt", "app/.env", ".envrc"} {
		if s := plan([]change{{path, modified}}); s != (steps{}) {
			t.Errorf("%s planned %+v, want nothing", path, s)
		}
	}
}

func TestFailedStepsCarryOver(t *testing.T) {
	cases := []struct {
		name    string
		first   []change
		fp      string
		fail    string
		ranNext []string
	}{
		{"go build", []change{{"app/a.go", modified}}, "v1", "GoBuild", []string{"Bundle", "GoBuild"}},
		{"analysis", []change{{"app/a.go", modified}}, "v1", "Analyze", []string{"Analyze", "Bundle", "GoBuild"}},
		{"generate", []change{{"app/a.go", modified}}, "v2", "Generate", []string{"Generate", "Bundle", "GoBuild"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p := built(t)
			p.fingerprint, p.fail = tc.fp, tc.fail
			_, err := b.build(context.Background(), plan(tc.first), nil)
			if err == nil {
				t.Fatal("build succeeded, want the error of " + tc.fail)
			}
			p.calls, p.fail = nil, ""
			if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}), nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.calls, tc.ranNext) {
				t.Fatalf("next build ran %v, want %v", p.calls, tc.ranNext)
			}
			p.calls = nil
			if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}), nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.calls, []string{"Bundle"}) {
				t.Fatalf("build after recovery ran %v, want only Bundle", p.calls)
			}
		})
	}
}

func TestBuildErrorNamesTheStep(t *testing.T) {
	b, p := built(t)
	p.fail = "GoBuild"
	_, err := b.build(context.Background(), plan([]change{{"app/a.go", modified}}), nil)
	if err == nil || err.Error() != "go build: GoBuild broke" {
		t.Fatalf("got %v, want the go build error", err)
	}
}

// Each step that runs leaves one line, so the terminal and tests can see what a change cost.
func TestBuildLogsEachStep(t *testing.T) {
	var lines []string
	p := &fakePipeline{fingerprint: "v1"}
	b := &builder{p: p, log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	if _, err := b.build(context.Background(), everything, nil); err != nil {
		t.Fatal(err)
	}
	lines = nil
	if _, err := b.build(context.Background(), plan([]change{{"app/a.go", modified}}), nil); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "static analysis done in ") ||
		lines[1] != "route manifest unchanged" || !strings.HasPrefix(lines[2], "go build done in ") {
		t.Fatalf("log %q", lines)
	}
}

// The bundle line also tells what took part of its time, such as Tailwind.
func TestBuildLogsTheBundleDetail(t *testing.T) {
	var lines []string
	p := &fakePipeline{fingerprint: "v1"}
	b := &builder{p: p, log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	if _, err := b.build(context.Background(), everything, nil); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []string{"", "tailwind 75ms"} {
		lines, p.detail = nil, detail
		if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}), nil); err != nil {
			t.Fatal(err)
		}
		want := `^bundle done in \d+m?s$`
		if detail != "" {
			want = `^bundle done in \d+m?s \(tailwind 75ms\)$`
		}
		if len(lines) != 1 || !regexp.MustCompile(want).MatchString(lines[0]) {
			t.Errorf("log %q, want %s", lines, want)
		}
	}
}
