package foreign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func lines(fs []Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.String()
	}
	return out
}

func TestScanTreeFindsTheFilesAndHandlersOfOtherFrameworks(t *testing.T) {
	got, err := ScanTree("testdata/idioms")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		file    string
		line    int
		warning bool
		text    string
	}{
		{"app/handlers/route.go", 11, false, "GET has the signature of a gin handler"},
		{"app/handlers/route.go", 13, false, "POST has the signature of an Echo handler"},
		{"app/handlers/route.go", 15, false, "PUT has the signature of a Fiber handler"},
		{"app/handlers/route.go", 17, false, "PATCH has the signature of a net/http handler"},
		{"app/middleware.ts", 1, true, "middleware.ts is the middleware file of Next.js"},
		{"app/next/error.tsx", 1, true, "error.tsx is the error file of Next.js"},
		{"app/next/loading.tsx", 1, true, "loading.tsx is the loading file of Next.js"},
		{"app/next/not-found.tsx", 1, true, "not-found.tsx is the not-found file of Next.js"},
		{"app/next2/layout.jsx", 1, true, "layout.jsx is the layout file of Next.js"},
		{"app/next2/page.js", 1, true, "page.js is the page file of Next.js"},
		{"app/next2/route.ts", 1, true, "route.ts is the route handler file of Next.js"},
		{"app/remix/$id.tsx", 1, true, "$id.tsx is a flat route file of Remix"},
		{"app/remix/_index.tsx", 1, false, "_index.tsx is the index route file of Remix"},
		{"app/svelte/+page.server.ts", 1, false, "+page.server.ts is a file of SvelteKit"},
		{"app/svelte/+page.svelte", 1, false, "+page.svelte is a file of SvelteKit"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d:\n%s", len(got), len(want), strings.Join(lines(got), "\n"))
	}
	for i, w := range want {
		f := got[i]
		if f.File != w.file || f.Line != w.line || f.Warning != w.warning || !strings.HasPrefix(f.Msg, w.text) {
			t.Errorf("finding %d is %q, want %s:%d (warning %v) starting %q", i, f.String(), w.file, w.line, w.warning, w.text)
		}
		if !strings.Contains(f.Msg, ". In Tifa: ") || !strings.Contains(f.Msg, ". See docs/") {
			t.Errorf("finding %d lacks the Tifa way or the doc: %q", i, f.Msg)
		}
	}
}

// What Tifa or Go allows is never reported: files that sit beside the Tifa file of the same name,
// components named error or loading in folders that are no routes, methods and helpers that take a
// net/http request.
func TestScanTreeLeavesLegalLookAlikesAlone(t *testing.T) {
	got, err := ScanTree("testdata/clean")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a clean app has findings:\n%s", strings.Join(lines(got), "\n"))
	}
}

func TestFolderNamesOfOtherFrameworks(t *testing.T) {
	for name, want := range map[string]string{
		"[id]":        "id+",
		"[...slug]":   "slug++",
		"[[...slug]]": "slug++",
		"$id":         "id+",
		":id":         "id+",
		"(marketing)": "route groups",
		"@modal":      "parallel routes",
	} {
		msg, ok := Folder(name)
		if !ok || !strings.Contains(msg, want) || !strings.Contains(msg, ". In Tifa: ") {
			t.Errorf("Folder(%q) = %q, %v, want a message with %q", name, msg, ok, want)
		}
	}
	for _, name := range []string{"orders", "order+", "path++", "_components", "a b", "[]", "$", "(", "@"} {
		if msg, ok := Folder(name); ok {
			t.Errorf("Folder(%q) = %q, want nothing", name, msg)
		}
	}
}

// Every doc a message points to has the section it names.
func TestEveryMessagePointsToASectionThatExists(t *testing.T) {
	slug := func(heading string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(strings.Trim(heading, "#"))) {
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteByte('-')
			}
		}
		return b.String()
	}
	anchors := map[string]map[string]bool{}
	for _, doc := range []string{docFiles, docFolders, docRouteGo, docPage, docLink, docLayout, docHandlers, docCalling, docBrowser, docCtx, docCache,
		docSSR, docErrors, docNotFound, docHooks, docWaiting, docContent, docPrivate} {
		file, anchor, _ := strings.Cut(doc, "#")
		if anchors[file] == nil {
			anchors[file] = map[string]bool{}
			data, err := os.ReadFile(filepath.Join("..", "..", file))
			if err != nil {
				t.Fatal(err)
			}
			inCode := false
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "```") {
					inCode = !inCode
				}
				if !inCode && strings.HasPrefix(line, "#") {
					anchors[file][slug(line)] = true
				}
			}
		}
		if !anchors[file][anchor] {
			t.Errorf("%s has no section %q", file, anchor)
		}
	}
}
