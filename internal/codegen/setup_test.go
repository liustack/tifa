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

// setup.ts at the app root is the first import of both entries, before the routes, so its top
// level runs once per JavaScript runtime before any route module evaluates.
func TestSetupIsTheFirstImportOfTheEntries(t *testing.T) {
	root := t.TempDir()
	m := pageApp(t, root)
	writeFile(t, filepath.Join(root, "setup.ts"), "import 'dayjs'\nexport const ignored = 1\n")
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for name, first := range map[string]string{"server.ts": `import "./runtime/host"`, "client.ts": `import "../setup"`} {
		text := string(files[name])
		setup := strings.Index(text, `import "../setup"`)
		routes := strings.Index(text, `from "./routes"`)
		if setup < 0 || routes < 0 || setup > routes {
			t.Errorf("%s does not import setup.ts before the routes:\n%s", name, text)
		}
		if !strings.Contains(text, first) {
			t.Errorf("%s lacks %s:\n%s", name, first, text)
		}
		if strings.Contains(text, "onStart") {
			t.Errorf("%s still passes onStart:\n%s", name, text)
		}
	}
	// The host comes first in the server entry, as the runtime needs its globals.
	server := string(files["server.ts"])
	if strings.Index(server, `import "./runtime/host"`) > strings.Index(server, `import "../setup"`) {
		t.Errorf("server.ts imports setup.ts before the host:\n%s", server)
	}
	// What setup.ts exports is not Tifa's business.
	if strings.Contains(string(files["client.ts"]), "ignored") {
		t.Errorf("client.ts names an export of setup.ts")
	}
}

// Without setup.ts the entries import nothing of the app root.
func TestEntriesWithoutSetup(t *testing.T) {
	root := t.TempDir()
	files, err := Generate(root, pageApp(t, root), false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server.ts", "client.ts"} {
		if strings.Contains(string(files[name]), "setup") {
			t.Errorf("%s:\n%s", name, files[name])
		}
	}
}

// A hooks.ts at the app root is the old name of setup.ts, and the build says how to move it.
func TestHooksTsFails(t *testing.T) {
	root := t.TempDir()
	m := pageApp(t, root)
	writeFile(t, filepath.Join(root, "hooks.ts"), "export function onStart() {}\n")
	_, err := Generate(root, m, false, fakeExports)
	want := "hooks.ts is now setup.ts: rename it and move the body of onStart to the top level of the file"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// An app without pages has no JavaScript to run setup.ts in, so the build says so.
func TestSetupWithoutPagesFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "setup.ts"), "console.log(1)\n")
	_, err := Generate(root, &analyze.Manifest{Module: "example.com/x"}, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "setup.ts") || !strings.Contains(err.Error(), "no JavaScript runs it") {
		t.Fatalf("got %v, want an error that names setup.ts", err)
	}
}
