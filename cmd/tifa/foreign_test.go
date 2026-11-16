package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

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
		"app/next2/page.js:1: page.js is the page file of Next.js. In Tifa: Tifa reads only page.tsx for a page and layout.tsx for a Layout. See docs/app.md#the-files-of-a-folder (warning)",
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
