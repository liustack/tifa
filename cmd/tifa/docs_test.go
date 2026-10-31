package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTifa writes a module that stands in for a downloaded tifa, with docs in every shape the
// index has to handle, and returns its folder.
func fakeTifa(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/liustack/tifa\n\ngo 1.27.0\n")
	docs := filepath.Join(dir, "docs")
	writeFile(t, filepath.Join(docs, "app.md"), "---\nsummary: 'Writing an app'\nread_when:\n  - Writing a page\n  - \"Adding a route\"\n---\n\n# App\n")
	writeFile(t, filepath.Join(docs, "app.zh-CN.md"), "---\nsummary: 'Chinese version'\nread_when: [x]\n---\n")
	writeFile(t, filepath.Join(docs, "config.md"), "---\nsummary: Settings\nread_when: [Changing a setting, 'Reading tifa.yaml']\nsomething_else: 1\n---\n")
	writeFile(t, filepath.Join(docs, "no-front-matter.md"), "# Nothing\n")
	writeFile(t, filepath.Join(docs, "archive", "old.md"), "---\nsummary: Old\n---\n")
	writeFile(t, filepath.Join(docs, "research", "idea.md"), "---\nsummary: Idea\n---\n")
	writeFile(t, filepath.Join(docs, "studies", "speed.md"), "---\nsummary: 'How fast it is'\n---\n")
	writeFile(t, filepath.Join(docs, "studies-2.md"), "---\nsummary: Sorted by path\nread_when:\n  - Checking the order\nother:\n  - not a read_when item\n---\n")
	return dir
}

// fakeApp writes an app whose go.mod replaces tifa with the module in tifaDir.
func fakeApp(t *testing.T, tifaDir string) string {
	t.Helper()
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/app\n\ngo 1.27.0\n\nrequire github.com/liustack/tifa v0.0.0\n\nreplace github.com/liustack/tifa => "+tifaDir+"\n")
	return app
}

func TestDocsIndexListsSummaryAndWhenToRead(t *testing.T) {
	index, warnings, err := docsIndex(fakeTifa(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `docs/app.md - Writing an app
  Read when: Writing a page | Adding a route
docs/config.md - Settings
  Read when: Changing a setting | Reading tifa.yaml
docs/studies-2.md - Sorted by path
  Read when: Checking the order
docs/studies/speed.md - How fast it is
`
	if index != want {
		t.Errorf("index:\n%s\nwant:\n%s", index, want)
	}
	if len(warnings) != 1 || warnings[0] != "docs/no-front-matter.md has no front matter summary" {
		t.Errorf("warnings %q", warnings)
	}
}

// The index reads the docs the way scripts/docs-list.sh does, so an agent in an app sees what an
// agent in the tifa repository sees.
func TestDocsIndexMatchesDocsListScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	root := filepath.Join("..", "..")
	script, err := exec.Command(bash, filepath.Join(root, "scripts", "docs-list.sh")).Output()
	if err != nil {
		t.Fatal(err)
	}
	want, _, found := strings.Cut(string(script), "\nReminder:")
	if !found {
		t.Fatalf("docs-list.sh printed no reminder:\n%s", script)
	}
	got, warnings, err := docsIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %q", warnings)
	}
	if got != want {
		t.Errorf("index differs from docs-list.sh\n--- docs\n%s\n--- script\n%s", got, want)
	}
}

// tifa docs prints where the docs of the tifa module the app uses are, also when go.mod replaces
// the module with a folder, and then the index.
func TestDocsPrintsTheDocsOfTheModuleTheAppUses(t *testing.T) {
	tifaDir := fakeTifa(t)
	app := fakeApp(t, tifaDir)
	out, stderr, err := run(t, "docs", "--dir", app)
	if err != nil {
		t.Fatalf("docs: %v\n%s", err, stderr)
	}
	for _, want := range []string{
		"github.com/liustack/tifa v0.0.0",
		"Docs: " + filepath.Join(tifaDir, "docs"),
		"docs/app.md - Writing an app\n  Read when: Writing a page | Adding a route\n",
		"docs/studies/speed.md - How fast it is\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"Chinese version", "Old", "Idea", "no-front-matter"} {
		if strings.Contains(out, not) {
			t.Errorf("the output has %q:\n%s", not, out)
		}
	}
	if !strings.Contains(stderr, "docs/no-front-matter.md has no front matter summary") {
		t.Errorf("no warning for the doc without front matter:\n%s", stderr)
	}
}

func TestDocsNeedsTifaInGoMod(t *testing.T) {
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	_, _, err := run(t, "docs", "--dir", app)
	if err == nil || !strings.Contains(err.Error(), "github.com/liustack/tifa") {
		t.Errorf("an app that does not require tifa: %v", err)
	}
}

func TestModuleWithoutDownloadedFilesSaysWhatToRun(t *testing.T) {
	_, err := parseModule([]byte(`{"Path": "github.com/liustack/tifa", "Version": "v1.2.3"}`))
	if err == nil || !strings.Contains(err.Error(), "go mod download github.com/liustack/tifa") {
		t.Errorf("a module with no Dir: %v", err)
	}
}
