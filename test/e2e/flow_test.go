package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// flowFiles extend a copy of test/apps/pages with the failures the error flow must handle in
// the real QuickJS VM: a page component that throws, a NotFound that never returns, a Layout
// Load that refuses, a middleware that redirects and one that refuses on any error of Next.
var flowFiles = map[string]string{
	"tifa.yaml": "ssr:\n  timeout: 1s\n",
	"hooks.go": `package pagestest

import (
	"fmt"
	"strings"

	"github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx) error {
	if c.Request().URL.Path == "/members" {
		if _, err := c.Cookie("session"); err != nil {
			return c.Redirect("/", 302)
		}
	}
	err := c.Next()
	if err != nil && c.Query("strict") != "" && strings.Contains(err.Error(), "permission denied") {
		return tifa.Error{Status: 403, Message: "refused by the middleware"}
	}
	return err
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	fmt.Println("pagestest: OnError:", strings.ReplaceAll(err.Error(), "\n", " "))
	return tifa.Error{Status: 500, Message: "reported"}
}
`,
	"app/throws/route.go":   "package throws\n\nconst Cache = true\n",
	"app/throws/route.tsx":  "export default function Throws(): never {\n  throw new Error('PAGE_THROWS')\n}\n",
	"app/legacy/legacy.cjs": "module.exports = function (name) {\n  return require(name)\n}\n",
	"app/legacy/route.tsx":  "import load from './legacy.cjs'\n\nexport default function Legacy() {\n  return <p>{String(load('node:os'))}</p>\n}\n",
	"app/ssr/route.tsx":     "export default function Page() {\n  return <p>PAGE_SSR</p>\n}\n",
	"app/members/route.tsx": "export default function Members() {\n  return <p>MEMBERS</p>\n}\n",
	"app/secret/layout.go": `package secret

import (
	"errors"

	"github.com/liustack/tifa"
)

func Layout(c *tifa.Ctx) (string, error) {
	if c.Query("deny") != "" {
		return "", errors.New("permission denied")
	}
	return "allowed", nil
}
`,
	"app/secret/route.go": `package secret

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return "PRIVATE_SECRET", nil }
`,
	"app/secret/route.tsx": "import { route } from './+route'\n\nexport default function Secret() {\n  return <p>{route.useLoaderData()}</p>\n}\n",
	"app/forbidden/route.go": `package forbidden

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return "", tifa.Error{Status: 403, Message: "no entry"} }
`,
	"app/forbidden/route.tsx": `import { RouteError } from 'tifa/client'
import { route } from './+route'

export default function Forbidden() {
  return <p>{route.useLoaderData()}</p>
}

export function Error({ error }: { error: unknown }) {
  return <p>{error instanceof RouteError ? 'FORBIDDEN_' + error.status : 'OTHER'}</p>
}
`,
}

func TestErrorFlowInTheVM(t *testing.T) {
	skipUnlessEnabled(t)
	dir := devApp(t)
	for name, content := range flowFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The root NotFound never returns, so the 404 page times out in the VM.
	layout := filepath.Join(dir, "app", "layout.tsx")
	data, err := os.ReadFile(layout)
	if err != nil {
		t.Fatal(err)
	}
	looping := strings.Replace(string(data), "export function NotFound() {\n", "export function NotFound() {\n  for (;;) {}\n", 1)
	if looping == string(data) {
		t.Fatal("app/layout.tsx has no NotFound to change")
	}
	if err := os.WriteFile(layout, []byte(looping), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := buildAt(t, dir)
	base, _, output := start(t, exe)
	var b struct {
		BuildID string `json:"buildId"`
	}
	readJSON(t, filepath.Join(dir, ".tifa", "dist", "build.json"), &b)
	onErrors := func() int { return strings.Count(output.String(), "pagestest: OnError:") }

	t.Run("a component that throws is an unexpected error and never cached", func(t *testing.T) {
		before := onErrors()
		for i := range 2 {
			code, header, body := get(t, base+"/throws")
			if code != 500 || header.Get("X-Tifa-Render") == "cache" || strings.Contains(body, "PAGE_THROWS") {
				t.Errorf("request %d: %d %v %s", i, code, header, body)
			}
		}
		output.waitFor(t, func(string) bool { return onErrors()-before >= 2 })
		if got := onErrors() - before; got != 2 {
			t.Errorf("OnError ran %d times:\n%s", got, output)
		}
		// React leaves the route's Suspense boundary to the browser, the server still learns why.
		if !regexp.MustCompile(`Error: PAGE_THROWS +at \w+ \(app/throws/route\.tsx:2:`).MatchString(output.String()) {
			t.Errorf("OnError did not see the error and its place:\n%s", output)
		}
	})

	t.Run("a require in the render names its place and what to do", func(t *testing.T) {
		before := onErrors()
		if code, _, body := get(t, base+"/legacy"); code != 500 {
			t.Errorf("%d %s", code, body)
		}
		output.waitFor(t, func(string) bool { return onErrors()-before >= 1 })
		want := "The code at app/legacy/legacy.cjs:2:10 calls require. SSR runs in QuickJS, which has no require"
		if !strings.Contains(output.String(), want) {
			t.Errorf("OnError:\n%s\nwant %s", output, want)
		}
	})

	t.Run("a failed error page goes to OnError", func(t *testing.T) {
		before := onErrors()
		start := time.Now()
		code, _, body := get(t, base+"/nowhere")
		if code != 500 || !strings.Contains(body, "reported") {
			t.Errorf("%d %s", code, body)
		}
		output.waitFor(t, func(out string) bool { return onErrors()-before >= 1 && strings.Contains(out, "render the 404 page") })
		if onErrors()-before != 1 {
			t.Errorf("OnError:\n%s", output)
		}
		if time.Since(start) > 10*time.Second {
			t.Errorf("the 404 took %s", time.Since(start))
		}
	})

	t.Run("a router redirect carries no ETag", func(t *testing.T) {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		etag := ""
		for i := range 2 {
			req, err := http.NewRequest("GET", base+"/%73sr", nil)
			if err != nil {
				t.Fatal(err)
			}
			if etag != "" {
				req.Header.Set("If-None-Match", etag)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 307 || res.Header.Get("Location") != "/ssr" || res.Header.Get("ETag") != "" {
				t.Errorf("request %d: %d %v", i, res.StatusCode, res.Header)
			}
			_, header, _ := get(t, base+"/ssr")
			etag = header.Get("ETag")
		}
	})

	dataHeaders := []string{"X-Tifa-Route", "app/layout,app/secret/layout,app/secret/route", "X-Tifa-Build", b.BuildID}
	t.Run("a middleware refuses the data request as it refuses the page", func(t *testing.T) {
		code, _, body := get(t, base+"/secret?deny=1&strict=1")
		if code != 403 || strings.Contains(body, "PRIVATE_SECRET") {
			t.Errorf("page: %d %s", code, body)
		}
		code, _, body = get(t, base+"/secret?deny=1&strict=1", dataHeaders...)
		if code != 403 || strings.Contains(body, "PRIVATE_SECRET") || !strings.Contains(body, "refused by the middleware") {
			t.Errorf("data: %d %s", code, body)
		}
		// Passed through unchanged, the data request is answered by route.
		code, _, body = get(t, base+"/secret?deny=1", dataHeaders...)
		var got map[string]struct{ Status int }
		if err := json.Unmarshal([]byte(body), &got); err != nil || code != 200 || got["app/secret/layout"].Status != 500 || got["app/secret/route"].Status != 200 {
			t.Errorf("passed through: %d %s", code, body)
		}
	})

	t.Run("a middleware redirect reaches a data request as JSON", func(t *testing.T) {
		code, header, body := get(t, base+"/members", "X-Tifa-Route", "app/layout", "X-Tifa-Build", b.BuildID)
		if code != 200 || header.Get("Location") != "" || strings.TrimSpace(body) != `{"redirect":"/","status":302}` {
			t.Errorf("%d %v %s", code, header, body)
		}
	})

	t.Run("a RouteError is dehydrated with its status", func(t *testing.T) {
		code, _, body := get(t, base+"/forbidden")
		if code != 403 || !strings.Contains(body, "FORBIDDEN_403") || !strings.Contains(body, `"tifa.RouteError"`) {
			t.Errorf("%d %s", code, body)
		}
	})
}
