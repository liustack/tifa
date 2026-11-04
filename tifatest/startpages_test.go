package tifatest_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

// pageBundle renders the JSON texts of the Loads of a page into a paragraph, as a stand-in for the
// SSR bundle of tifa build.
const pageBundle = `globalThis.startRender = (input, ...texts) => {
	globalThis.renderResult = '<!DOCTYPE html><p>' + texts.join('|') + '</p>';
};`

// builtApp writes an app root with the front end of a build in .tifa/dist, and moves the test
// into it. inputs is the content of inputs.json, none when empty.
func builtApp(t *testing.T, inputs string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                 "module example.com/pages\n",
		"app/page.tsx":           "export default function Page() { return null }\n",
		".tifa/dist/build.json":  `{"buildId":"b1","head":"<script type=\"module\" src=\"/assets/app.js\"></script>"}`,
		".tifa/dist/ssr.js":      pageBundle,
		".tifa/dist/assets/a.js": "console.log(1)",
		// The collections that the content tests of this binary declare read it.
		".tifa/dist/content.json": "{}",
	}
	if inputs != "" {
		files[".tifa/dist/inputs.json"] = inputs
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

func pageApp() server.App {
	return server.App{Routes: []server.Route{{
		ID: "app/route", Path: "/", Page: true, SSR: true, Layouts: []string{},
		Load: func(c *tifa.Ctx) (any, error) {
			return tifa.Use(c, func(c *tifa.Ctx) (string, error) { return "hi " + c.Query("name"), nil })
		},
	}}}
}

// Pages render to HTML with the bundle of .tifa/dist, also in a later test: the VMs outlive the
// server of the test that made them.
func TestStartPagesRendersPages(t *testing.T) {
	builtApp(t, `{"dirs": ["public", "content"], "files": {}}`)
	for _, name := range []string{"Ann", "Bo"} {
		t.Run(name, func(t *testing.T) {
			srv := tifatest.StartPages(t, pageApp())
			res := get(t, srv, "/?name="+name)
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 || !strings.Contains(string(body), `<p>"hi `+name+`"</p>`) {
				t.Fatalf("page: %d %s", res.StatusCode, body)
			}
			if res := get(t, srv, "/assets/a.js"); res.StatusCode != 200 {
				t.Errorf("a file of the bundle: %d", res.StatusCode)
			}
		})
	}
}

func TestStartPagesWantsABuild(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module example.com/none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fatal{TB: t}
	if srv := tifatest.StartPages(f, pageApp()); srv != nil || !strings.Contains(f.msg, "run tifa build") {
		t.Errorf("without .tifa/dist: %v, %q", srv, f.msg)
	}
}

func TestStartPagesWantsTheRecordOfTheInputs(t *testing.T) {
	builtApp(t, "")
	f := &fatal{TB: t}
	if srv := tifatest.StartPages(f, pageApp()); srv != nil || !strings.Contains(f.msg, "inputs.json") {
		t.Errorf("without inputs.json: %v, %q", srv, f.msg)
	}
}

// A file that changed after the build fails the test with its name.
func TestStartPagesFailsOnAFrontEndOlderThanItsSources(t *testing.T) {
	builtApp(t, `{"dirs": ["public", "content"], "files": {"app/page.tsx": "00000000000000000000000000000000", "components/gone.tsx": "00000000000000000000000000000000"}}`)
	f := &fatal{TB: t}
	srv := tifatest.StartPages(f, pageApp())
	if srv != nil || !strings.Contains(f.msg, "app/page.tsx, components/gone.tsx") || !strings.Contains(f.msg, "tifa build") {
		t.Errorf("an old build: %v, %q", srv, f.msg)
	}
}
