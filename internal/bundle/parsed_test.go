package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// tifa dev parses the files of an app on every bundle, and most of them did not change. A file whose
// content was parsed before keeps its result, and only the others go to the parser, in their order,
// so a parse error is the one a parse of every file gives.
func TestParsedReadsOnlyChangedFiles(t *testing.T) {
	dir := t.TempDir()
	file := func(name string) string { return filepath.Join(dir, name) }
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(file(name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var asked [][]string
	// parse gives the content of each file, leaves out a file that holds "skip" and fails at the
	// first that holds "bad", as the parses of the driver do.
	parse := func(files []string) (map[string]string, error) {
		asked = append(asked, slices.Clone(files))
		out := map[string]string{}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			switch text := string(data); {
			case strings.HasPrefix(text, "bad"):
				return nil, errors.New("cannot parse " + filepath.Base(f))
			case text != "skip":
				out[f] = text
			}
		}
		return out, nil
	}
	var c parsed[string]
	read := func(want map[string]string, wantAsked []string, names ...string) {
		t.Helper()
		var files []string
		for _, n := range names {
			files = append(files, file(n))
		}
		asked = nil
		got, err := c.read(files, parse)
		if err != nil {
			t.Fatal(err)
		}
		wantFull := map[string]string{}
		for n, v := range want {
			wantFull[file(n)] = v
		}
		if !reflect.DeepEqual(got, wantFull) {
			t.Fatalf("got %v, want %v", got, wantFull)
		}
		var wantFiles [][]string
		if wantAsked != nil {
			var w []string
			for _, n := range wantAsked {
				w = append(w, file(n))
			}
			wantFiles = [][]string{w}
		}
		if !reflect.DeepEqual(asked, wantFiles) {
			t.Fatalf("parsed %v, want %v", asked, wantFiles)
		}
	}
	write("a.tsx", "a1")
	write("b.tsx", "b1")
	write("c.tsx", "skip")
	read(map[string]string{"a.tsx": "a1", "b.tsx": "b1"}, []string{"a.tsx", "b.tsx", "c.tsx"}, "a.tsx", "b.tsx", "c.tsx")
	read(map[string]string{"a.tsx": "a1", "b.tsx": "b1"}, nil, "a.tsx", "b.tsx", "c.tsx")
	write("b.tsx", "b2")
	read(map[string]string{"a.tsx": "a1", "b.tsx": "b2"}, []string{"b.tsx"}, "a.tsx", "b.tsx", "c.tsx")
	read(map[string]string{"b.tsx": "b2"}, nil, "b.tsx")

	// The error is the one of the first file that fails, as without the results kept.
	write("a.tsx", "bad a")
	write("b.tsx", "bad b")
	for range 2 {
		if _, err := c.read([]string{file("a.tsx"), file("b.tsx")}, parse); err == nil || err.Error() != "cannot parse a.tsx" {
			t.Fatalf("got %v", err)
		}
	}
	write("a.tsx", "a1")
	if _, err := c.read([]string{file("a.tsx"), file("b.tsx"), file("c.tsx")}, parse); err == nil || err.Error() != "cannot parse b.tsx" {
		t.Fatalf("got %v", err)
	}
	// A file that cannot be read goes to the parser, which says why.
	asked = nil
	if _, err := c.read([]string{file("a.tsx"), file("gone.tsx")}, parse); err == nil || !errors.Is(err, os.ErrNotExist) || len(asked) != 1 || !slices.Equal(asked[0], []string{file("gone.tsx")}) {
		t.Fatalf("got %v after parsing %v", err, asked)
	}
}
