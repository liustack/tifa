package codegen

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
	"golang.org/x/mod/modfile"
)

// pgoApp writes an app with one page, or with one API route when pages is false, and the given
// files, and returns its root and manifest.
func pgoApp(t *testing.T, pages bool, files map[string]string) (string, *analyze.Manifest) {
	t.Helper()
	root := t.TempDir()
	route := analyze.Route{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}
	if pages {
		writeFile(t, filepath.Join(root, "app/page/route.tsx"), "export default function P() { return null }\n")
	} else {
		route = analyze.Route{ID: "app/api/route", Dir: "app/api", Package: "example.com/x/app/api", Path: "/api", Handlers: []analyze.Handler{{Method: "GET"}}, SSR: true}
	}
	for name, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	return root, &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{route}}
}

// tifa build writes the profile go build optimizes with into .tifa/default.pgo, where the go
// command's default -pgo=auto finds it next to the main package. Each case has its own app.
func TestBuildProfile(t *testing.T) {
	own := "app profile\n"
	for _, tc := range []struct {
		name  string
		pages bool
		files map[string]string
		want  []byte // nil: no default.pgo
	}{
		{"pages use the engine profile", true, nil, enginePGO},
		{"the app's default.pgo wins", true, map[string]string{"default.pgo": own}, []byte(own)},
		{"no pages, no profile", false, nil, nil},
		{"no pages with the app's default.pgo", false, map[string]string{"default.pgo": own}, []byte(own)},
		{"off", true, map[string]string{"default.pgo": own, "tifa.yaml": "build:\n  pgo: off\n"}, nil},
		{"a file", true, map[string]string{"default.pgo": "unused\n", "profiles/prod.pgo": own, "tifa.yaml": "build:\n  pgo: profiles/prod.pgo\n"}, []byte(own)},
		{"auto said out loud", true, map[string]string{"tifa.yaml": "build:\n  pgo: auto\n"}, enginePGO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, m := pgoApp(t, tc.pages, tc.files)
			files, err := Generate(root, m, false, fakeExports)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := files["default.pgo"]
			if tc.want == nil {
				if ok {
					t.Errorf("generated default.pgo of %d bytes", len(got))
				}
				return
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("default.pgo holds %d bytes %.20q, want %d bytes %.20q", len(got), got, len(tc.want), tc.want)
			}
		})
	}
}

// tifa dev builds without PGO, whatever the app has, so a rebuild never compiles everything again
// for a new profile.
func TestDevBuildsWithoutProfile(t *testing.T) {
	root, m := pgoApp(t, true, map[string]string{"default.pgo": "app profile\n", "profiles/prod.pgo": "x", "tifa.yaml": "build:\n  pgo: profiles/prod.pgo\n"})
	files, err := Generate(root, m, true, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["default.pgo"]; ok {
		t.Error("tifa dev got default.pgo")
	}
}

func TestMissingProfileFails(t *testing.T) {
	root, m := pgoApp(t, true, map[string]string{"tifa.yaml": "build:\n  pgo: profiles/prod.pgo\n"})
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "build.pgo") || !strings.Contains(err.Error(), filepath.Join("profiles", "prod.pgo")) {
		t.Errorf("a missing profile: %v", err)
	}
}

// build.pgo is relative to the app root. An absolute path would tie the project file to one
// machine, so it fails with what to write instead.
func TestAbsoluteProfilePathFails(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "prod.pgo")
	writeFile(t, abs, "app profile\n")
	root, m := pgoApp(t, true, map[string]string{"tifa.yaml": "build:\n  pgo: " + strconv.Quote(abs) + "\n"})
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "build.pgo") || !strings.Contains(err.Error(), "relative to the app root") {
		t.Errorf("an absolute path: %v", err)
	}
}

// PGO finds a call site by the caller, the callee and the line of the call within the caller, so
// the engine profile must come from the version of slowjs that tifa requires, on linux/amd64. See
// "The engine profile" in docs/testing.md for how to collect it again.
func TestEngineProfileMatchesTheDependencies(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	const slowjs = "github.com/liustack/slowjs"
	version := ""
	for _, r := range mod.Require {
		if r.Mod.Path == slowjs {
			version = r.Mod.Version
		}
	}
	if version == "" {
		t.Fatalf("go.mod does not require %s", slowjs)
	}
	files, err := profileFiles(enginePGO)
	if err != nil {
		t.Fatalf("engine.pgo: %v", err)
	}
	// Each source file the profile has samples in, such as
	// /home/me/go/pkg/mod/github.com/liustack/slowjs@v0.29.0/vm.go. The engine itself is assembly,
	// which has no .go file: the Go side of slowjs is what the profile finds call sites in.
	prefix := slowjs + "@" + version + "/"
	found := false
	for _, file := range files {
		if !strings.Contains(file, slowjs+"@") {
			continue
		}
		if !strings.Contains(file, prefix) {
			t.Errorf("engine.pgo has samples in %s, want the files of %s", file, prefix)
			continue
		}
		found = true
	}
	if !found {
		t.Errorf("engine.pgo has no samples in the files of %s", prefix)
	}
}

// profileFiles returns the source file names of a gzipped pprof profile: the strings of its
// string table that end in .go.
func profileFiles(profile []byte) ([]string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(profile))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var files []string
	for len(raw) > 0 {
		key, n := binary.Uvarint(raw)
		if n <= 0 {
			return nil, errors.New("bad field key")
		}
		raw = raw[n:]
		switch key & 7 {
		case 0: // varint
			if _, n = binary.Uvarint(raw); n <= 0 {
				return nil, errors.New("bad varint")
			}
			raw = raw[n:]
		case 1: // 64-bit
			raw = raw[min(8, len(raw)):]
		case 5: // 32-bit
			raw = raw[min(4, len(raw)):]
		case 2: // length-delimited
			size, n := binary.Uvarint(raw)
			if n <= 0 || uint64(len(raw)-n) < size {
				return nil, errors.New("bad length")
			}
			value := string(raw[n : n+int(size)])
			raw = raw[n+int(size):]
			if key>>3 == 6 && strings.HasSuffix(value, ".go") { // Profile.string_table
				files = append(files, value)
			}
		default:
			return nil, fmt.Errorf("wire type %d", key&7)
		}
	}
	return files, nil
}
