package devserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakePipeline records the steps it runs. fingerprint is what Analyze returns, fail names a step that fails.
type fakePipeline struct {
	calls       []string
	fingerprint string
	fail        string
	detail      string // what Bundle adds to its log line
	// goBuild, when set, holds GoBuild until it is closed.
	goBuild chan struct{}
	// embedded is what Embedded returns, nothing when nil.
	embedded func(string) bool
	// content is what HasContent returns. contents records the arguments of each Content.
	content  bool
	contents []string
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
func (p *fakePipeline) Content(_ context.Context, all bool, changed []string) (string, error) {
	p.contents = append(p.contents, fmt.Sprintf("%v %v", all, changed))
	return "", p.step("Content")
}
func (p *fakePipeline) HasContent() bool { return p.content }
func (p *fakePipeline) GoBuild(context.Context) error {
	if p.goBuild != nil {
		<-p.goBuild
	}
	return p.step("GoBuild")
}
func (p *fakePipeline) PollFiles(context.Context) (bool, error) { return true, nil }
func (p *fakePipeline) Embedded() func(string) bool {
	if p.embedded == nil {
		return func(string) bool { return false }
	}
	return p.embedded
}

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
		{"tsx", []change{{"app/orders/list/page.tsx", modified}}, "v1", []string{"Bundle"}},
		{"ts", []change{{"app/orders/format.ts", modified}}, "v1", []string{"Bundle"}},
		{"css", []change{{"app/app.css", created}}, "v1", []string{"Bundle"}},
		{"new component", []change{{"app/orders/list/items.tsx", created}}, "v1", []string{"Bundle"}},
		{"go body", []change{{"app/orders/store.go", modified}}, "v1", []string{"Analyze", "GoBuild"}},
		{"go.mod", []change{{"go.mod", modified}}, "v1", []string{"Analyze", "GoBuild"}},
		{"go routes", []change{{"app/orders/list/route.go", modified}}, "v2", all},
		{"new route", []change{{"app/orders/new/page.tsx", created}}, "v1", all},
		{"removed route", []change{{"app/orders/new/page.tsx", removed}}, "v1", all},
		{"removed layout", []change{{"app/orders/layout.tsx", removed}}, "v1", all},
		{"new replacing layout", []change{{"app/orders/layout_.tsx", created}}, "v1", all},
		{"replacing layout", []change{{"app/orders/layout_.tsx", modified}}, "v1", []string{"Bundle"}},
		{"config", []change{{"tifa.yaml", modified}}, "v1", all},
		{"env", []change{{".env", modified}}, "v1", nil},
		{"loaded from outside the root", []change{{"../shared/value.txt", modified}}, "v1", []string{"Bundle"}},
		{"go and tsx", []change{{"app/orders/store.go", modified}, {"app/app.css", modified}}, "v1", []string{"Analyze", "Bundle", "GoBuild"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p := built(t)
			p.fingerprint = tc.fingerprint
			ran, err := b.build(context.Background(), plan(tc.changes, nil), nil)
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
				restart:  tc.name == "env",
			}
			if ran != want {
				t.Fatalf("build returned %+v, want %+v", ran, want)
			}
		})
	}
}

// A file under content/ needs the content step alone: no analysis and no go build. In an app with
// content the step also follows every analysis, and a restart of Vite's dev server converts every
// entry, as the Vite config or its plugins may have changed.
func TestRebuildContent(t *testing.T) {
	cases := []struct {
		name     string
		changes  []change
		content  bool // HasContent
		want     []string
		contents []string
	}{
		{"post", []change{{"content/blog/en/a/index.md", modified}}, true, []string{"Content"}, []string{"false [content/blog/en/a/index.md]"}},
		{"new post", []change{{"content/blog/en/b/index.md", created}}, true, []string{"Content"}, []string{"false [content/blog/en/b/index.md]"}},
		{"image", []change{{"content/blog/zh-CN/a/diagram.png", modified}}, true, []string{"Content"}, []string{"false [content/blog/zh-CN/a/diagram.png]"}},
		{"new folder without collections", []change{{"content/notes/a.md", created}}, false, []string{"Content"}, []string{"false [content/notes/a.md]"}},
		// Whatever its name, a file under content/ is content, such as one a body shows.
		{"json of a body", []change{{"content/blog/en/a/data.json", modified}}, true, []string{"Content"}, []string{"false [content/blog/en/a/data.json]"}},
		{"css of a body", []change{{"content/blog/en/a/styles.css", modified}}, true, []string{"Content"}, []string{"false [content/blog/en/a/styles.css]"}},
		{"page.tsx under content", []change{{"content/blog/en/a/page.tsx", created}}, true, []string{"Content"}, []string{"false [content/blog/en/a/page.tsx]"}},
		{"go under content", []change{{"content/blog/en/a/main.go", modified}}, true, []string{"Content"}, []string{"false [content/blog/en/a/main.go]"}},
		{"content.go", []change{{"content.go", modified}}, true, []string{"Analyze", "Content", "GoBuild"}, []string{"false []"}},
		{"go without content", []change{{"app/a.go", modified}}, false, []string{"Analyze", "GoBuild"}, nil},
		{"vite restarted", []change{{viteServer, restarted}}, true, []string{"Bundle", "Content"}, []string{"true []"}},
		{"vite restarted without content", []change{{viteServer, restarted}}, false, []string{"Bundle"}, nil},
		{"post and tsx", []change{{"content/blog/en/a/index.md", modified}, {"app/a.tsx", modified}}, true, []string{"Bundle", "Content"}, []string{"false [content/blog/en/a/index.md]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p := built(t)
			p.content, p.contents = tc.content, nil
			b.noteContent(tc.changes)
			ran, err := b.build(context.Background(), plan(tc.changes, nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.calls, tc.want) || !slices.Equal(p.contents, tc.contents) {
				t.Fatalf("ran %v with %q, want %v with %q", p.calls, p.contents, tc.want, tc.contents)
			}
			if inPlace := !slices.Contains(tc.want, "Analyze"); ran.inPlace() != inPlace {
				t.Errorf("ran %+v, in place %v, want %v", ran, ran.inPlace(), inPlace)
			}
		})
	}
}

// A content step that fails runs again with the next rebuild, with the files it had.
func TestFailedContentCarriesOver(t *testing.T) {
	b, p := built(t)
	p.content, p.fail = true, "Content"
	first := []change{{"content/blog/en/a/index.md", modified}}
	b.noteContent(first)
	if _, err := b.build(context.Background(), plan(first, nil), nil); err == nil {
		t.Fatal("build succeeded, want the error of Content")
	}
	p.calls, p.contents, p.fail = nil, nil, ""
	next := []change{{"app/a.tsx", modified}}
	b.noteContent(next)
	if _, err := b.build(context.Background(), plan(next, nil), nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Bundle", "Content"}; !slices.Equal(p.calls, want) || !slices.Equal(p.contents, []string{"false [content/blog/en/a/index.md]"}) {
		t.Fatalf("next build ran %v with %q", p.calls, p.contents)
	}
}

func TestPlanIgnoresOtherFiles(t *testing.T) {
	for _, path := range []string{"README.md", "app/logo.png", "app/tifa.yaml", "app/notes.txt", "app/.env", ".envrc", ".env.local", ".env.production"} {
		if s := plan([]change{{path, modified}}, nil); s != (steps{}) {
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
			_, err := b.build(context.Background(), plan(tc.first, nil), nil)
			if err == nil {
				t.Fatal("build succeeded, want the error of " + tc.fail)
			}
			p.calls, p.fail = nil, ""
			if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}, nil), nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.calls, tc.ranNext) {
				t.Fatalf("next build ran %v, want %v", p.calls, tc.ranNext)
			}
			p.calls = nil
			if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}, nil), nil); err != nil {
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
	_, err := b.build(context.Background(), plan([]change{{"app/a.go", modified}}, nil), nil)
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
	if _, err := b.build(context.Background(), plan([]change{{"app/a.go", modified}}, nil), nil); err != nil {
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
		if _, err := b.build(context.Background(), plan([]change{{"app/a.tsx", modified}}, nil), nil); err != nil {
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

// A slow step says it is still running, the first time with why a first go build is slow, and
// stops saying so once it is done.
func TestBuildLogsTheProgressOfASlowStep(t *testing.T) {
	var log syncBuffer
	p := &fakePipeline{fingerprint: "v1", goBuild: make(chan struct{})}
	b := &builder{p: p, log: func(format string, args ...any) { fmt.Fprintf(&log, format+"\n", args...) }, slow: 20 * time.Millisecond, every: 30 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		_, err := b.build(context.Background(), everything, nil)
		done <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); strings.Count(log.String(), "go build still running") < 2; {
		if time.Now().After(deadline) {
			t.Fatalf("no progress lines within 5s:\n%s", log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(p.goBuild)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	var progress []string
	for _, line := range lines {
		if strings.Contains(line, "still running") {
			progress = append(progress, line)
		}
	}
	if !regexp.MustCompile(`^go build still running \(\d+s\)\. The first build compiles QuickJS and the other dependencies, later builds use Go's build cache$`).MatchString(progress[0]) {
		t.Errorf("first progress line %q", progress[0])
	}
	for _, line := range progress[1:] {
		if !regexp.MustCompile(`^go build still running \(\d+s\)$`).MatchString(line) {
			t.Errorf("progress line %q", line)
		}
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "go build done in ") {
		t.Errorf("a progress line came after the step ended:\n%s", log.String())
	}
	// Fast steps print no progress.
	if strings.Contains(log.String(), "static analysis still running") || strings.Contains(log.String(), "bundle still running") {
		t.Errorf("a fast step printed progress:\n%s", log.String())
	}
}

// A file the Go code embeds with go:embed is part of the program: a change builds it again, and
// the file need not be Go. The analysis is only needed when the set of embedded files may change,
// so a file that appears or disappears.
func TestPlanEmbeddedFiles(t *testing.T) {
	embedded := func(p string) bool { return strings.HasPrefix(p, "app/posts/") || p == "app/schema.json" }
	cases := []struct {
		name   string
		change change
		want   steps
	}{
		{"edited markdown", change{"app/posts/a.md", modified}, steps{goBuild: true}},
		{"new markdown", change{"app/posts/b.md", created}, steps{analyze: true, goBuild: true}},
		{"removed markdown", change{"app/posts/a.md", removed}, steps{analyze: true, goBuild: true}},
		{"markdown nothing embeds", change{"docs/a.md", modified}, steps{}},
		// Also a file for the front end: both the bundle and the program change.
		{"embedded json", change{"app/schema.json", modified}, steps{bundle: true, goBuild: true}},
		{"new embedded json", change{"app/schema.json", created}, steps{analyze: true, bundle: true, goBuild: true}},
		{"json nothing embeds", change{"app/other.json", modified}, steps{bundle: true}},
		{"embedded tsx", change{"app/posts/Card.tsx", modified}, steps{bundle: true, goBuild: true}},
		// A page that is also embedded changes the routes as it would anyway.
		{"embedded page", change{"app/posts/page.tsx", created}, everything},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := plan([]change{tc.change}, embedded); got != tc.want {
				t.Errorf("planned %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The builder asks the pipeline which files the program embeds after each analysis that worked,
// and not when the analysis failed: the last answer stays.
func TestBuilderLearnsEmbeddedFilesFromTheAnalysis(t *testing.T) {
	p := &fakePipeline{fingerprint: "v1", embedded: func(p string) bool { return p == "a.md" }}
	var learned []func(string) bool
	b := &builder{p: p, log: t.Logf, learn: func(f func(string) bool) { learned = append(learned, f) }}
	if _, err := b.build(context.Background(), steps{bundle: true}, nil); err != nil {
		t.Fatal(err)
	}
	if len(learned) != 0 {
		t.Fatal("learned before any analysis")
	}
	if _, err := b.build(context.Background(), everything, nil); err != nil {
		t.Fatal(err)
	}
	if len(learned) != 1 || !learned[0]("a.md") || learned[0]("b.md") {
		t.Fatalf("learned %d times from the first analysis", len(learned))
	}
	p.fail = "Analyze"
	if _, err := b.build(context.Background(), steps{analyze: true}, nil); err == nil {
		t.Fatal("the analysis should fail")
	}
	if len(learned) != 1 {
		t.Fatal("learned from an analysis that failed")
	}
}

func (p *fakePipeline) Public() string { return "public" }
