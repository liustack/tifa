package codegen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
)

// pageApp writes an app with one page under root and returns its manifest.
func pageApp(t *testing.T, root string) *analyze.Manifest {
	t.Helper()
	writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export default function P() { return null }\n")
	return &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}}}
}

// The onStart of hooks.ts at the app root reaches both entries, which run it before the first
// render.
func TestHooksReachTheEntries(t *testing.T) {
	root := t.TempDir()
	m := pageApp(t, root)
	writeFile(t, filepath.Join(root, "hooks.ts"), "export function onStart() {}\n")
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]string{"server.ts": "serve(routeTree, htmlAttributes, onStart)", "client.ts": "hydrate(routeTree, htmlAttributes, onStart)"} {
		text := string(files[name])
		if !strings.Contains(text, `import { onStart } from "../hooks"`) || !strings.Contains(text, call) {
			t.Errorf("%s does not run onStart of hooks.ts:\n%s", name, text)
		}
	}
}

// Without hooks.ts the entries import nothing of the app root.
func TestEntriesWithoutHooks(t *testing.T) {
	root := t.TempDir()
	files, err := Generate(root, pageApp(t, root), false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server.ts", "client.ts"} {
		if strings.Contains(string(files[name]), "hooks") {
			t.Errorf("%s:\n%s", name, files[name])
		}
	}
}

// hooks.ts exports only the hooks Tifa runs, so a misspelt one fails the build instead of never
// running.
func TestHooksWithAnotherExportFails(t *testing.T) {
	root := t.TempDir()
	m := pageApp(t, root)
	writeFile(t, filepath.Join(root, "hooks.ts"), "export function onStart() {}\nexport function onstart() {}\n")
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "hooks.ts") || !strings.Contains(err.Error(), "onstart") || !strings.Contains(err.Error(), "onStart") {
		t.Fatalf("got %v, want an error that names hooks.ts, the export and onStart", err)
	}
}

// An app without pages has no JavaScript to run hooks.ts in, so the build says so.
func TestHooksWithoutPagesFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hooks.ts"), "export function onStart() {}\n")
	_, err := Generate(root, &analyze.Manifest{Module: "example.com/x"}, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "hooks.ts") {
		t.Fatalf("got %v, want an error that names hooks.ts", err)
	}
}
