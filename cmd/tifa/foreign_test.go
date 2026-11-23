package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/foreign"
)

// An app with Go handlers of other frameworks stops at the analysis. Each idiom is reported with the
// Tifa way, before the messages of the analysis, and the warnings go to stderr too.
func TestCheckSaysWhatTheTifaWayIsForOtherFrameworksHandlers(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "foreign", "testdata", "idioms")
	out, stderr, err := run(t, "check", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "check failed at analyze: ") {
		t.Fatalf("check: %v\n%s", err, out)
	}
	for _, want := range []string{
		"app/handlers/route.go:11: GET has the signature of a gin handler. In Tifa: ",
		"app/svelte/+page.svelte:1: +page.svelte is a file of SvelteKit. In Tifa: ",
		"app/next2/page.js:1: page.js is the page file of Next.js. In Tifa: Tifa reads only page.tsx for a page and layout.tsx for a Layout. See docs/start/app.md#the-files-of-a-folder (warning)",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(out, "==> generate") {
		t.Errorf("generation started after the errors:\n%s", out)
	}
}

// Warnings alone do not stop a check. The errors of the TypeScript do, after the files are read.
func TestCheckForeignFailsOnErrorsAndPrintsWarnings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app", "page.tsx"), "")
	writeFile(t, filepath.Join(root, "app", "util.ts"), "")
	page, util := filepath.Join(root, "app", "page.tsx"), filepath.Join(root, "app", "util.ts")
	var asked []string
	read := func(files []string) (map[string]*foreign.Facts, error) {
		asked = files
		return map[string]*foreign.Facts{
			page: {Directives: []foreign.Directive{{Value: "use client", Line: 1, Column: 1}}},
			util: {Imports: []foreign.Import{{Spec: "next/link", Line: 3, Column: 1}}},
		}, nil
	}
	var warnings bytes.Buffer
	err := checkForeign(root, true, &warnings, read)
	if err == nil || !strings.HasPrefix(err.Error(), "app/util.ts:3: next/link is a module of Next.js. In Tifa: ") {
		t.Fatalf("err = %v", err)
	}
	if got := warnings.String(); !strings.HasPrefix(got, "app/page.tsx:1: 'use client' is a directive of React Server Components") || !strings.HasSuffix(got, "(warning)\n") {
		t.Errorf("warnings = %q", got)
	}
	if len(asked) != 2 {
		t.Errorf("read %v", asked)
	}
	// Only warnings: nothing fails.
	read = func([]string) (map[string]*foreign.Facts, error) {
		return map[string]*foreign.Facts{page: {Directives: []foreign.Directive{{Value: "use client", Line: 1, Column: 1}}}}, nil
	}
	if err := checkForeign(root, true, nil, read); err != nil {
		t.Errorf("a warning failed the check: %v", err)
	}
	// An app without pages reads nothing.
	asked = nil
	if err := checkForeign(root, false, nil, func(f []string) (map[string]*foreign.Facts, error) { asked = f; return nil, nil }); err != nil || asked != nil {
		t.Errorf("without pages: %v, read %v", err, asked)
	}
}

// The analysis warns of a number above 2^53 put into a field a page reads. tifa build and tifa
// check print it, and tifa dev prints it once until it changes.
func TestAnalyzeAppPrintsWarningsOfBigNumbers(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	if _, err := analyzeApp(cmd, filepath.Join("..", "..", "internal", "analyze", "testdata", "bigsources")); err != nil {
		t.Fatal(err)
	}
	want := `app/api/route.go:22:55: Mine.ID gets a value from time.Time.UnixNano, a number above 2^53.`
	if !strings.Contains(stderr.String(), want) || !strings.Contains(stderr.String(), "(warning)") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
	var dev bytes.Buffer
	p := &devPipeline{warn: &dev}
	m, err := analyze.Analyze(filepath.Join("..", "..", "internal", "analyze", "testdata", "bigsources"))
	if err != nil {
		t.Fatal(err)
	}
	p.printWarnings(m.Warnings)
	first := dev.Len()
	p.printWarnings(m.Warnings)
	if first == 0 || dev.Len() != first {
		t.Errorf("tifa dev printed %d bytes, then %d", first, dev.Len())
	}
}

// tifa dev keeps the warnings of its checks for tifa errors: those of the analysis, of the idiom
// check of the files and of the content. A later run of a check replaces its own.
func TestDevPipelineKeepsItsWarnings(t *testing.T) {
	var dev bytes.Buffer
	p := &devPipeline{warn: &dev}
	m, err := analyze.Analyze(filepath.Join("..", "..", "internal", "analyze", "testdata", "bigsources"))
	if err != nil {
		t.Fatal(err)
	}
	p.setWarnings("analysis", analysisWarnings(m))
	err = p.collect("foreign files", func(w io.Writer) error {
		_, err := scanForeign(filepath.Join("..", "..", "internal", "foreign", "testdata", "idioms"), w)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(p.Warnings(), "\n")
	for _, want := range []string{"Mine.ID gets a value from time.Time.UnixNano", "middleware.ts is the middleware file of Next.js"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	for _, line := range p.Warnings() {
		if !strings.HasSuffix(line, "(warning)") {
			t.Errorf("%q does not end in (warning)", line)
		}
	}
	if !strings.Contains(dev.String(), "middleware.ts is the middleware file of Next.js") {
		t.Errorf("the dev output lost the warning:\n%s", dev.String())
	}
	// A run that finds nothing takes its warnings away, the others stay.
	p.setWarnings("foreign files", nil)
	if got := strings.Join(p.Warnings(), "\n"); strings.Contains(got, "middleware.ts") || !strings.Contains(got, "UnixNano") {
		t.Errorf("after the check ran clean:\n%s", got)
	}
}

func TestWarnPublicImportsPrintsTheWarningsOfAnAppWithPages(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "foreign", "testdata", "publicuse")
	var out strings.Builder
	if err := warnPublicImports(root, "public", true, &out); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "(warning)"); n != 4 {
		t.Errorf("got %d warnings, want 4:\n%s", n, out.String())
	}
	out.Reset()
	if err := warnPublicImports(root, "public", false, &out); err != nil || out.Len() != 0 {
		t.Errorf("an app without pages got %q, %v", out.String(), err)
	}
}
