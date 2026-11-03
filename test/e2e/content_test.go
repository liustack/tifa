package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestContentApp builds test/apps/content, a blog and docs in two languages declared in its
// content.go, and checks the content collections from outside: the build warns about missing
// translations and leaves the draft out of the program, the list pages show the posts that can be
// seen now and their translations, the RSS feed is written by a handler from the same posts, the
// docs pages read single entries, and the app's own Go tests read the content with tifatest.
func TestContentApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := filepath.Join(repoRoot(t), "test", "apps", "content")
	exe, out := buildWithOutput(t, dir, os.Getenv("PATH"))
	for _, want := range []string{
		"tifa: content: 1 entry of blog has no zh-CN version: 202609/second (warning)",
		"tifa: content: 2 entries of docs have no zh-CN version: guide, guide/routing (warning)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the build did not print %q:\n%s", want, out)
		}
	}
	for _, file := range []string{filepath.Join(dir, ".tifa", "dist", "content.json"), exe} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("DRAFT-TEXT-MUST-NOT-SHIP")) || bytes.Contains(data, []byte("写了一半的草稿")) {
			t.Errorf("%s holds the draft", file)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, ".tifa", "dist", "content.json"))
	if err != nil {
		t.Fatal(err)
	}
	// An entry whose date is still to come is in the program, and hidden when it runs.
	if !bytes.Contains(data, []byte("Release notes")) {
		t.Errorf("content.json lacks the post still to come:\n%s", data)
	}

	base, _, _ := start(t, exe)

	t.Run("list", func(t *testing.T) {
		code, _, body := get(t, base+"/en/blog")
		if code != 200 {
			t.Fatalf("GET /en/blog: %d\n%s", code, body)
		}
		second, first := strings.Index(body, "A second post"), strings.Index(body, "Free web search for your model")
		if second < 0 || first < 0 || second > first {
			t.Errorf("the newest post does not come first:\n%s", body)
		}
		for _, want := range []string{`data-slug="free-web-search"`, "zh-CN en", `<html lang="en"`} {
			if !strings.Contains(body, want) {
				t.Errorf("/en/blog lacks %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "Release notes") {
			t.Errorf("/en/blog shows a post whose date is still to come:\n%s", body)
		}
		code, _, body = get(t, base+"/zh-CN/blog")
		if code != 200 || !strings.Contains(body, "免费给模型加上联网搜索") || !strings.Contains(body, `data-slug="free-search"`) {
			t.Errorf("GET /zh-CN/blog: %d\n%s", code, body)
		}
		for _, hidden := range []string{"写了一半的草稿", "新版本发布"} {
			if strings.Contains(body, hidden) {
				t.Errorf("/zh-CN/blog shows %q", hidden)
			}
		}
		if code, _, _ := get(t, base+"/fr/blog"); code != 404 {
			t.Errorf("GET /fr/blog: %d", code)
		}
	})

	t.Run("rss", func(t *testing.T) {
		code, header, body := get(t, base+"/en/rss.xml")
		if code != 200 || header.Get("Content-Type") != "application/rss+xml; charset=utf-8" {
			t.Fatalf("GET /en/rss.xml: %d %q\n%s", code, header.Get("Content-Type"), body)
		}
		if n := strings.Count(body, "<item>"); n != 2 {
			t.Errorf("%d items, want 2:\n%s", n, body)
		}
		for _, want := range []string{
			"<link>https://example.com/en/blog/free-web-search</link>",
			"<pubDate>Sun, 02 Aug 2026 00:00:00 +0000</pubDate>",
			"<title>A second post</title>",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the feed lacks %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "Release notes") {
			t.Errorf("the feed has a post whose date is still to come:\n%s", body)
		}
		if code, _, _ := get(t, base+"/fr/rss.xml"); code != 404 {
			t.Errorf("GET /fr/rss.xml: %d", code)
		}
	})

	t.Run("docs", func(t *testing.T) {
		code, _, body := get(t, base+"/en/docs")
		if code != 200 || !strings.Contains(body, `<h1 id="doc">Docs</h1>`) {
			t.Fatalf("GET /en/docs: %d\n%s", code, body)
		}
		install, guide, routing := strings.Index(body, ">Install<"), strings.Index(body, ">Guide<"), strings.Index(body, ">Routing<")
		if install < 0 || guide < install || routing < guide {
			t.Errorf("the side bar is not in the order of the docs:\n%s", body)
		}
		if code, _, body := get(t, base+"/en/docs/guide/routing"); code != 200 || !strings.Contains(body, `<h1 id="doc">Routing</h1>`) {
			t.Errorf("GET /en/docs/guide/routing: %d\n%s", code, body)
		}
		if code, _, _ := get(t, base+"/zh-CN/docs/guide"); code != 404 {
			t.Errorf("GET /zh-CN/docs/guide, which has no Chinese version: %d", code)
		}
	})

	t.Run("go test", func(t *testing.T) {
		cmd := exec.Command("go", "test", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test ./... in the app: %v\n%s", err, out)
		}
	})
	typecheck(t, dir)
}
