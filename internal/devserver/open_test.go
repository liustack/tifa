package devserver

import (
	"os"
	"path/filepath"
	"testing"
)

// While the watcher hashes a file, an editor may save it again by writing a new file and renaming
// it over the old one, or delete it. Neither may fail because the watcher has the file open.
func TestAFileBeingHashedCanBeReplacedAndDeleted(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "page.tsx")
	writeFile(t, dir, "page.tsx", "v1")
	f, err := openToHash(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeFile(t, dir, "page.tsx.tmp", "v2")
	if err := os.Rename(filepath.Join(dir, "page.tsx.tmp"), name); err != nil {
		t.Fatalf("an atomic save while the file is hashed: %v", err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatalf("a delete while the file is hashed: %v", err)
	}
	// The open file still reads what it had.
	buf := make([]byte, 8)
	if n, err := f.Read(buf); err != nil || string(buf[:n]) != "v1" {
		t.Errorf("read %q, %v, want v1", buf[:n], err)
	}
}
