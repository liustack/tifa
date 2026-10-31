package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTypeScript writes a typescript package whose package.json names bin/tsc as tsc.
func writeTypeScript(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"name": "typescript", "version": "6.0.3", "bin": {"tsc": "./bin/tsc", "tsserver": "./bin/tsserver"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "tsc"), []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The compiler is the tsc of the typescript package the app installs, found the way Node finds
// it: in a flat node_modules, behind a pnpm link, and in the node_modules of a folder above.
func TestTypeScriptCompilerIsTheOneTheAppInstalls(t *testing.T) {
	flat := t.TempDir()
	writeTypeScript(t, filepath.Join(flat, "node_modules", "typescript"))

	pnpm := t.TempDir()
	store := filepath.Join(pnpm, "node_modules/.pnpm/typescript@6.0.3/node_modules/typescript")
	writeTypeScript(t, store)
	symlink(t, store, filepath.Join(pnpm, "node_modules/typescript"))

	workspace := t.TempDir()
	writeTypeScript(t, filepath.Join(workspace, "node_modules", "typescript"))
	member := filepath.Join(workspace, "packages", "web")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct{ root, pkg string }{
		"npm":       {flat, filepath.Join(flat, "node_modules", "typescript")},
		"pnpm":      {pnpm, store},
		"workspace": {member, filepath.Join(workspace, "node_modules", "typescript")},
	} {
		got, err := TypeScriptCompiler(tc.root)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		want, err := filepath.EvalSymlinks(filepath.Join(tc.pkg, "bin", "tsc"))
		if err != nil {
			t.Fatal(err)
		}
		gotReal, err := filepath.EvalSymlinks(got)
		if err != nil || gotReal != want {
			t.Errorf("%s: compiler %s (%v), want %s", name, got, err, want)
		}
	}
}

func TestTypeScriptCompilerNeedsTheInstall(t *testing.T) {
	_, err := TypeScriptCompiler(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "typescript is not installed, run the install of your package manager in the app root") {
		t.Errorf("no node_modules: %v", err)
	}
	root := t.TempDir()
	writePackage(t, filepath.Join(root, "node_modules", "typescript"), "typescript", "6.0.3") // no bin
	if _, err := TypeScriptCompiler(root); err == nil || !strings.Contains(err.Error(), "no tsc") {
		t.Errorf("a typescript package without tsc: %v", err)
	}
}
