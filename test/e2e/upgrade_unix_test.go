//go:build unix

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

const tifaPath = "github.com/liustack/tifa"

// treeFile is a file of the checkout that goes into a module zip.
type treeFile struct {
	path, abs string
	data      []byte // when set, the zip holds this instead of the file
}

func (f treeFile) Path() string                 { return f.path }
func (f treeFile) Lstat() (os.FileInfo, error)  { return os.Lstat(f.abs) }
func (f treeFile) Open() (io.ReadCloser, error) { return os.Open(f.abs) }

type memFile struct {
	treeFile
}

func (f memFile) Lstat() (os.FileInfo, error)  { return memInfo{f.path, int64(len(f.data))}, nil }
func (f memFile) Open() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(f.data)), nil }

type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return path.Base(i.name) }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() os.FileMode  { return 0o644 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// writeTifaProxy writes a module proxy folder with the checkout as the module github.com/liustack/tifa
// at v0.0.901 (a go.mod only, the app starts there) and a version made from the files (the whole module, with a CHANGELOG.md
// of its own), so tifa upgrade has a release to move to without the network. The examples and the
// tests of the checkout are left out: they are other modules, or no part of what an app uses.
func writeTifaProxy(t *testing.T, changelog string) (proxy, version string) {
	t.Helper()
	root := repoRoot(t)
	list := func(args ...string) []string {
		cmd := exec.Command("git", append([]string{"ls-files", "-z"}, args...)...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git ls-files: %v", err)
		}
		return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
	}
	nested := map[string]bool{}
	for _, m := range list("--cached", "--others", "--exclude-standard", "*/go.mod") {
		nested[path.Dir(m)] = true
	}
	var files []modzip.File
	// The version is made from the files, so that a module cache never holds an older tree under it.
	sum := fnv.New32a()
	for _, name := range list("--cached", "--others", "--exclude-standard") {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			continue // deleted in the working tree, not yet staged
		}
		skip := strings.HasPrefix(name, "examples/") || strings.HasPrefix(name, "test/")
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			skip = skip || nested[dir]
		}
		if skip {
			continue
		}
		f := treeFile{path: name, abs: filepath.Join(root, name)}
		if data, err := os.ReadFile(f.abs); err == nil {
			sum.Write([]byte(name))
			sum.Write(data)
		}
		if name == "CHANGELOG.md" {
			continue // the zip gets the changelog of the test
		}
		files = append(files, f)
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	version = fmt.Sprintf("v0.0.%d", 1000+sum.Sum32()%1000000000)
	proxy = filepath.Join(t.TempDir(), "proxy")
	files = append(files, memFile{treeFile{path: "CHANGELOG.md", data: []byte(strings.ReplaceAll(changelog, "{NEW}", strings.TrimPrefix(version, "v")))}})
	dir := filepath.Join(proxy, filepath.FromSlash(tifaPath), "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"v0.0.901", version} {
		write(v+".info", []byte(`{"Version":"`+v+`","Time":"2026-11-17T00:00:00Z"}`))
		write(v+".mod", gomod)
	}
	write("list", []byte("v0.0.901\n"+version+"\n"))
	var zipped bytes.Buffer
	if err := modzip.Create(&zipped, module.Version{Path: tifaPath, Version: version}, files); err != nil {
		t.Fatalf("zip %s: %v", version, err)
	}
	write(version+".zip", zipped.Bytes())
	return proxy, version
}

const upgradeChangelog = `# Changelog

## [Unreleased]

- Not released. Upgrading: you will not see this.

## [{NEW}] - 2026-11-18

### Changed

- A page reads its parameter in the Load. Upgrading: move the read of ` + "`utm`" + ` into the Load.
- Something with no instruction.

## [0.0.901] - 2026-11-17

- The release the app has. Upgrading: you will not see this either.
`

// readTree returns the content of the files of the app that tifa upgrade must not touch: its own
// code, which is every file of app/ and the Go files and SQL of the root.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if err == nil && !d.IsDir() && (strings.HasPrefix(rel, "app/") || !strings.Contains(rel, "/") && (strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, ".sql") || rel == "tifa.yaml")) {
			data, _ := os.ReadFile(p)
			files[rel] = string(data)
		}
		return nil
	})
	return files
}

// The versions are far above the releases there are, so that no module cache holds them.
// TestUpgrade moves an app from Tifa v0.0.901 to v0.0.902, both served by a proxy folder on disk: the
// old tifa (the one built in this checkout) runs go get, the new one, built by go tool from v0.0.902,
// does the rest. A pnpm that only writes down how it was called stands in for the network.
func TestUpgrade(t *testing.T) {
	skipUnlessEnabled(t)
	modules := filepath.Join(repoRoot(t), "test", "apps", "pages", "node_modules")
	if _, err := os.Stat(modules); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "pnpm-called")
	fail := filepath.Join(bin, "pnpm-fails") // while this file exists, pnpm exits with 1
	if err := os.WriteFile(filepath.Join(bin, "pnpm"), []byte("#!/bin/sh\necho \"$@\" >> '"+called+"'\n[ -e '"+fail+"' ] && exit 1\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxy, newV := writeTifaProxy(t, upgradeChangelog)
	nv := func(s string) string { return strings.ReplaceAll(s, "v0.0.902", newV) }
	env := append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOPROXY=file://"+proxy+",https://proxy.golang.org,direct",
		"GOSUMDB=off", "GOFLAGS=-modcacherw", "GOWORK=off")
	run := func(dir string, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	dir := filepath.Join(t.TempDir(), "hello")
	if out, err := run(t.TempDir(), cli(t), "new", dir, "--module", "example.com/hello"); err != nil {
		t.Fatalf("tifa new: %v\n%s", err, out)
	}
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	// Make the app one that was written by an older Tifa: the older release in go.mod, older
	// packages in package.json, and a package of its own that stays as it is.
	fresh := read("package.json")
	old := strings.NewReplacer(`"react": "19.3.0"`, `"react": "19.2.0"`, `"vite": "8.3.4"`, `"vite": "8.0.0"`).Replace(fresh)
	old = strings.Replace(old, `"typescript": `, `"zod": "3.0.0",`+"\n    "+`"typescript": `, 1)
	if old == fresh {
		t.Fatal("the package.json of tifa new has none of the versions this test makes older")
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	gomod := strings.Replace(read("go.mod"), "github.com/liustack/tifa v0.0.0", "github.com/liustack/tifa v0.0.901", 1)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	before := readTree(t, dir)

	for version, want := range map[string]string{"v0.0.901": "already v0.0.901", "v0.0.5": "older than v0.0.901"} {
		if out, err := run(dir, cli(t), "upgrade", version); err == nil || !strings.Contains(out, want) {
			t.Errorf("upgrade %s: %v, want an error with %q\n%s", version, err, want, out)
		}
	}
	if read("go.mod") != gomod {
		t.Fatalf("a refused upgrade changed go.mod:\n%s", read("go.mod"))
	}

	// A step after go get fails: the exit status stays, and the output says how to go on.
	if err := os.WriteFile(fail, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(dir, cli(t), "upgrade", nv("v0.0.902"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("tifa upgrade with a failing pnpm: %v, want exit status 1\n%s", err, out)
	}
	for _, want := range []string{nv("==> tifa v0.0.901 to v0.0.902"), "react: 19.2.0 -> 19.3.0", "vite: 8.0.0 -> 8.3.4", "pnpm install failed", nv("Go is at v0.0.902"), "go tool tifa upgrade --after v0.0.901"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "go tool tifa upgrade --after v0.0.901\n"); n < 1 || strings.Count(out, "Go is at") != 1 {
		t.Errorf("the hint should come once:\n%s", out)
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	// Run what it says: the rest is done, and the old version in --after is what the notes start from.
	out, err = run(dir, "go", "tool", "tifa", "upgrade", "--after", "v0.0.901")
	if err != nil {
		t.Fatalf("go tool tifa upgrade --after v0.0.901: %v\n%s", err, out)
	}
	for _, want := range []string{
		nv("v0.0.902: move the read of `utm` into the Load."),
		"check passed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "you will not see this") {
		t.Errorf("the output has a note of a version it should not:\n%s", out)
	}
	if !strings.Contains(read("go.mod"), nv("github.com/liustack/tifa v0.0.902\n")) {
		t.Errorf(nv("go.mod does not require v0.0.902:\n%s"), read("go.mod"))
	}
	// Only the pinned packages changed, and zod stayed.
	if want := strings.Replace(fresh, `"typescript": `, `"zod": "3.0.0",`+"\n    "+`"typescript": `, 1); read("package.json") != want {
		t.Errorf("package.json:\n%s\nwant:\n%s", read("package.json"), want)
	}
	if got, _ := os.ReadFile(called); string(got) != "install\ninstall\n" {
		t.Errorf("pnpm was called with %q, want the failed install and the one after it", got)
	}
	if !strings.Contains(read("AGENTS.md"), "<!-- tifa:begin -->") || !strings.Contains(read(".claude/skills/tifa/SKILL.md"), nv("Written by Tifa v0.0.902 ")) {
		t.Errorf(nv("the agent files were not written by Tifa v0.0.902:\n%s"), read(".claude/skills/tifa/SKILL.md")[:400])
	}
	for name, was := range before {
		if read(name) != was {
			t.Errorf("tifa upgrade changed %s", name)
		}
	}

	// Run again: the app has v0.0.902 now.
	if out, err := run(dir, cli(t), "upgrade", nv("v0.0.902")); err == nil || !strings.Contains(out, nv("already v0.0.902")) {
		t.Errorf("a second upgrade: %v\n%s", err, out)
	}
}
