package tifatest_test

import (
	"io"
	"os"
	"testing"

	"github.com/liustack/tifa/tifatest"
)

// File makes the *multipart.FileHeader a handler gets for an uploaded file, with its name, size,
// content type and content.
func TestFile(t *testing.T) {
	big := make([]byte, 33<<20) // over what a form keeps in memory, so it lands in a temporary file
	for _, tc := range []struct {
		name, contentType string
		data              []byte
	}{
		{"sea.png", "image/png", []byte("png bytes")},
		{"notes", "application/octet-stream", []byte("text")},
		{"big.bin", "application/octet-stream", big},
	} {
		f := tifatest.File(t, tc.name, tc.data)
		if f.Filename != tc.name || f.Size != int64(len(tc.data)) || f.Header.Get("Content-Type") != tc.contentType {
			t.Errorf("%s: %q %d %q", tc.name, f.Filename, f.Size, f.Header.Get("Content-Type"))
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(b) != string(tc.data) {
			t.Errorf("%s: read %d bytes, %v", tc.name, len(b), err)
		}
	}
}

// The temporary files of File are gone once the test ends.
func TestFileRemovesItsTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Run("upload", func(t *testing.T) {
		tifatest.File(t, "big.bin", make([]byte, 33<<20))
		if entries, _ := os.ReadDir(dir); len(entries) == 0 {
			t.Fatal("no temporary file while the test runs")
		}
	})
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left %d files", len(entries))
	}
}
