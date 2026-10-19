package bundle

import (
	"bytes"
	"compress/gzip"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The text files of the browser bundle from 1024 bytes on get a .gz next to them, which reads
// back as the file. Smaller files, compressed formats and files that gzip would not make
// smaller get none. The same files give the same bytes.
func TestPrecompressWritesGzipFiles(t *testing.T) {
	random := make([]byte, 4096)
	r := rand.NewChaCha8([32]byte{1})
	r.Read(random)
	files := map[string]string{
		"app-1.js":        strings.Repeat("console.log('tifa');\n", 100),
		"chunk-1.mjs":     strings.Repeat("export const a = 1;\n", 100),
		"style-1.css":     strings.Repeat(".page { color: red }\n", 100),
		"logo-1.svg":      "<svg>" + strings.Repeat("<path d='M0 0'/>", 100) + "</svg>",
		"data-1.json":     "[" + strings.Repeat(`"tifa",`, 200) + `"end"]`,
		"small-1.js":      strings.Repeat("x", 1023),
		"font-1.woff2":    strings.Repeat("woff2", 1000),
		"photo-1.png":     strings.Repeat("png", 1000),
		"noise-1.js":      string(random),
		"nested/deep.css": strings.Repeat(".deep { color: blue }\n", 100),
	}
	want := map[string]bool{"app-1.js": true, "chunk-1.mjs": true, "style-1.css": true, "logo-1.svg": true, "data-1.json": true, "nested/deep.css": true}
	var first map[string][]byte
	for round := range 2 {
		dir := t.TempDir()
		writeTree(t, dir, files)
		if err := precompress(dir); err != nil {
			t.Fatal(err)
		}
		got := map[string][]byte{}
		for name, content := range files {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)+".gz"))
			if os.IsNotExist(err) {
				if want[name] {
					t.Errorf("%s has no .gz", name)
				}
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			if !want[name] {
				t.Errorf("%s has a .gz", name)
			}
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			plain, err := io.ReadAll(zr)
			if err != nil || string(plain) != content || len(data) >= len(content) {
				t.Errorf("%s.gz: %d bytes, reads %d bytes, %v", name, len(data), len(plain), err)
			}
			got[name] = data
		}
		if round == 1 {
			for name, data := range got {
				if !bytes.Equal(data, first[name]) {
					t.Errorf("%s.gz differs between two builds", name)
				}
			}
		}
		first = got
	}
	// An application without pages has no browser bundle.
	if err := precompress(filepath.Join(t.TempDir(), "assets")); err != nil {
		t.Errorf("missing folder: %v", err)
	}
}
