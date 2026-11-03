package e2e

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A copy of test/apps/pages gets a parameter folder at the root of app/ and a public file. Go
// answers /robots.txt with the file, but the route /{section} matches the path as well, so a
// Link to it must load the file and not ask for navigation data.
func TestLinkToAPublicFileLoadsTheFile(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "pages")
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("public/robots.txt", "User-agent: *\nDisallow:\n")
	// A Layout without a page at its URL cannot sit next to a root parameter folder.
	write("app/account/page.tsx", "export default function Account() {\n  return <p>account</p>\n}\n")
	write("app/article/page.tsx", "export default function Articles() {\n  return <p>articles</p>\n}\n")
	write("app/section+/route.go", `package section

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return c.Param("section"), nil }
`)
	write("app/section+/page.tsx", `import { Link } from '@tanstack/react-router'
import { page } from './route.gen'

export default function Section() {
  return (
    <main>
      <h1 id="section">{page.useLoaderData()}</h1>
      <Link id="file" to={'/robots.txt' as string}>icon</Link>
    </main>
  )
}
`)
	exe := buildAt(t, dir)
	base, _, _ := start(t, exe)

	resp, err := http.Get(base + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("/robots.txt: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if status, _, body := get(t, base+"/hello"); status != 200 || !strings.Contains(body, `id="section"`) {
		t.Fatalf("/hello: %d\n%.300s", status, body)
	}

	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	name := "e2e-pf"
	work := t.TempDir()
	pw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("playwright-cli", append([]string{"-s=" + name}, args...)...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	pw("open", base+"/hello")
	t.Cleanup(func() { exec.Command("playwright-cli", "-s="+name, "close").Run() })
	// The page records every fetch it makes in sessionStorage, which the file's document shares.
	pw("eval", `() => {
  const fetched = []
  const original = window.fetch
  window.fetch = (input, init) => {
    fetched.push(String(input instanceof Request ? input.url : input))
    sessionStorage.setItem("fetched", JSON.stringify(fetched))
    return original(input, init)
  }
  setTimeout(() => document.getElementById("file").click(), 50)
  return "clicked"
}`)
	// The tab list says where the browser is.
	var tabs string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		tabs = pw("tab-list")
		if strings.Contains(tabs, "/robots.txt") {
			break
		}
	}
	if !strings.Contains(tabs, base+"/robots.txt") {
		t.Fatalf("the Link did not load the file:\n%s", tabs)
	}
	if fetched := pw("sessionstorage-get", "fetched"); strings.Contains(fetched, "_tifa") || strings.Contains(fetched, "robots") {
		t.Fatalf("the click fetched navigation data:\n%s", fetched)
	}
}
