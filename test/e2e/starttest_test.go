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

	page := filepath.Join(dir, "app", "page.tsx")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	write(t, page, string(data)+"\nexport const changed = true\n")
	out, err = goTest()
	if err == nil || !strings.Contains(out, "app/page.tsx") || !strings.Contains(out, "run tifa build again") {
		t.Fatalf("go test after a page changed: %v\n%s", err, out)
	}
}
