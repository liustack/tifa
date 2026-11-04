package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pagesRequestTest is a test of the pages app that starts it with tifatest: once with its front end,
// which renders the home page, and once without, where the page answers the data of its Layout.
const pagesRequestTest = `package app_test

import (
	"io"
	"strings"
	"testing"

	"example.com/pagestest/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestHomeRenders(t *testing.T) {
	srv := tifatest.StartPages(t, tifaapp.App)
	res, err := srv.Client().Get(srv.URL + "/?lang=fr")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{"<title>Home</title>", "North shop", ` + "`" + `<div lang="fr">` + "`" + `} {
		if res.StatusCode != 200 || !strings.Contains(string(body), want) {
			t.Errorf("the home page has no %q: %d\n%s", want, res.StatusCode, body)
		}
	}
}

func TestHomeAnswersItsData(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	res, err := srv.Client().Get(srv.URL + "/?lang=fr")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if want := ` + "`" + `"app/layout":{"name":"Pages","lang":"fr"}` + "`" + `; res.StatusCode != 200 || !strings.Contains(string(body), want) {
		t.Errorf("the home page without a front end: %d %s, want %s", res.StatusCode, body, want)
	}
}
`

// A test of an app renders its pages with tifatest.StartPages once tifa build has run, and fails
// with what to do before the build and after a page changed.
func TestStartPagesInAnApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "pages")
	write(t, filepath.Join(dir, "app", "request_test.go"), pagesRequestTest)
	goTest := func() (string, error) {
		cmd := exec.Command("go", "test", "-count=1", "./app/")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// tifa check generates .tifa/tifaapp but does not bundle, so the test that renders fails.
	out, err := tifaCheck(t, dir)
	if err == nil || !strings.Contains(out, "check failed at go test") || !strings.Contains(out, "run tifa build first") {
		t.Fatalf("tifa check before a build: %v\n%s", err, out)
	}

	build := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "pages"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("tifa build: %v\n%s", err, out)
	}
	if out, err := goTest(); err != nil {
		t.Fatalf("go test after the build: %v\n%s", err, out)
	}

	// Each change below makes the front end older than its sources, and the test names the files
	// changed so far: a component that renders only in the browser, a public file added since the
	// build, and a page.
	for _, change := range []struct{ file, add string }{
		{"app/shops/shop+/items/item+/basket.client.tsx", "\nexport const changed = true\n"},
		{"public/added.txt", "new\n"},
		{"app/page.tsx", "\nexport const changed = true\n"},
	} {
		path := filepath.Join(dir, filepath.FromSlash(change.file))
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		write(t, path, string(data)+change.add)
		out, err := goTest()
		if err == nil || !strings.Contains(out, change.file) || !strings.Contains(out, "run tifa build again") {
			t.Fatalf("go test after a change to %s: %v\n%s", change.file, err, out)
		}
	}
}

// tailwindRequestTest renders the home page of the tailwind app with tifatest.StartPages.
const tailwindRequestTest = `package requests_test

import (
	"testing"

	"example.com/tailwindtest/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestHomeRenders(t *testing.T) {
	srv := tifatest.StartPages(t, tifaapp.App)
	res, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("the home page: %d", res.StatusCode)
	}
}
`

// The files a stylesheet reads without being a module are inputs of the build too: a stylesheet
// it imports, which Vite inlines, and a file Tailwind scans for class names. A change to either
// makes StartPages fail.
func TestStartPagesSeesTheFilesOfTheStylesheets(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "tailwind")
	replaceIn(t, filepath.Join(dir, "app", "site.css"), "@import \"tailwindcss\";\n", "@import \"tailwindcss\";\n@import \"./theme.css\";\n@source \"../extra\";\n")
	write(t, filepath.Join(dir, "app", "theme.css"), "@theme {\n  --color-brand: oklch(0.6 0.2 30);\n}\n")
	write(t, filepath.Join(dir, "extra", "classes.html"), "<p class=\"underline decoration-dotted\"></p>\n")
	write(t, filepath.Join(dir, "requests", "request_test.go"), tailwindRequestTest)
	goTest := func() (string, error) {
		cmd := exec.Command("go", "test", "-count=1", "./requests/")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	build := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "tailwind"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("tifa build: %v\n%s", err, out)
	}
	if out, err := goTest(); err != nil {
		t.Fatalf("go test after the build: %v\n%s", err, out)
	}
	for _, change := range []struct{ file, text string }{
		{"app/theme.css", "@theme {\n  --color-brand: oklch(0.5 0.2 30);\n}\n"},
		{"extra/classes.html", "<p class=\"underline decoration-wavy\"></p>\n"},
	} {
		write(t, filepath.Join(dir, filepath.FromSlash(change.file)), change.text)
		out, err := goTest()
		if err == nil || !strings.Contains(out, change.file) || !strings.Contains(out, "run tifa build again") {
			t.Fatalf("go test after a change to %s: %v\n%s", change.file, err, out)
		}
	}
}
