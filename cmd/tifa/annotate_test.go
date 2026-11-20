package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/foreign"
)

// Outside GitHub Actions nothing is written.
func TestAnnotateOnlyInGitHubActions(t *testing.T) {
	t.Setenv(envGitHubActions, "")
	var b bytes.Buffer
	annotate(&b, false, "app/a.go", 1, 2, "bad")
	if b.Len() != 0 {
		t.Errorf("wrote %q", b.String())
	}
}

// An error and a warning become workflow commands of their own kind, with the file, line and
// column, and the special characters escaped as GitHub asks.
func TestAnnotateEscapes(t *testing.T) {
	t.Setenv(envGitHubActions, "true")
	var b bytes.Buffer
	annotate(&b, false, "app/a,b:c.go", 3, 4, "100% wrong\nsecond line")
	annotate(&b, true, "app/page.tsx", 7, 0, "look")
	annotate(&b, true, "content/notes", 0, 0, "a folder")
	want := "::error file=app/a%2Cb%3Ac.go,line=3,col=4::100%25 wrong%0Asecond line\n" +
		"::warning file=app/page.tsx,line=7::look\n" +
		"::warning file=content/notes::a folder\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// The findings about TypeScript, which no matcher of setup-go or setup-node reads, are marked.
func TestReportAnnotatesFindings(t *testing.T) {
	t.Setenv(envGitHubActions, "true")
	var b bytes.Buffer
	errs := report([]foreign.Finding{
		{File: "app/page.tsx", Line: 2, Column: 5, Msg: "use client is Next.js"},
		{File: "app/page.tsx", Line: 9, Warning: true, Msg: "an image of public"},
	}, &b)
	if len(errs) != 1 {
		t.Fatalf("errs %q", errs)
	}
	for _, want := range []string{"::error file=app/page.tsx,line=2,col=5::use client is Next.js\n", "::warning file=app/page.tsx,line=9::an image of public\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lacks %q:\n%s", want, b.String())
		}
	}
	if strings.Contains(b.String(), "app/page.tsx:9") {
		t.Errorf("the warning is printed twice:\n%s", b.String())
	}
}
