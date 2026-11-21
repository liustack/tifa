package server

import (
	"mime"
	"testing"
)

// Go's mime package reads the types of the system: the registry on Windows, /etc/mime.types and
// the like elsewhere, and what they say wins over its own table. A machine with a wrong entry for
// .js must not change the Content-Type of the files of the front end.
func TestFileTypesDoNotDependOnTheSystem(t *testing.T) {
	want := map[string]string{
		".js":   "text/javascript; charset=utf-8",
		".mjs":  "text/javascript; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".html": "text/html; charset=utf-8",
		".json": "application/json",
		".svg":  "image/svg+xml",
		".wasm": "application/wasm",
	}
	t.Cleanup(pinFileTypes)
	for ext := range want {
		if err := mime.AddExtensionType(ext, "application/x-from-the-system"); err != nil {
			t.Fatal(err)
		}
	}
	pinFileTypes()
	for ext, w := range want {
		if got := mime.TypeByExtension(ext); got != w {
			t.Errorf("%s is served as %q, want %q", ext, got, w)
		}
	}
}
