package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

	// The body of each entry is a file of dist/content, without a compressed copy. No file of dist
	// holds the draft, which is never converted.
	var drafts, gz []string
	diagrams := 0
	err = filepath.WalkDir(filepath.Join(dir, ".tifa", "dist"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, filepath.Join(dir, ".tifa", "dist")+string(filepath.Separator)))
		if bytes.Contains(data, []byte("DRAFT-TEXT-MUST-NOT-SHIP")) || bytes.Contains(data, []byte("写了一半的草稿")) {
			drafts = append(drafts, rel)
		}
		if strings.HasPrefix(rel, "content/") && strings.HasSuffix(rel, ".gz") {
			gz = append(gz, rel)
		}
		if strings.HasPrefix(rel, "assets/diagram-") {
			diagrams++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) > 0 || len(gz) > 0 {
		t.Errorf("files of dist with the draft: %q, compressed bodies: %q", drafts, gz)
	}
	// The English post uses the image of the Chinese one, which is one file.
	if diagrams != 1 {
		t.Errorf("dist/assets has %d diagrams, want 1", diagrams)
	}
	html, err := os.ReadFile(filepath.Join(dir, ".tifa", "dist", "content", "blog", "zh-CN", "202608", "free-search.html"))
	if err != nil || !bytes.Contains(html, []byte(`<h2 id="怎么装">怎么装</h2>`)) {
		t.Errorf("the body of free-search: %v\n%s", err, html)
	}
	for _, want := range []string{
		`"file":"content/blog/zh-CN/202608/free-search.html"`,
		`"headings":[{"depth":2,"id":"怎么装","text":"怎么装"},{"depth":2,"id":"为什么免费","text":"为什么免费"},{"depth":3,"id":"搜索从哪来","text":"搜索从哪来"}]`,
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("content.json lacks %s:\n%s", want, data)
		}
	}

	base, _, _ := start(t, exe)

	t.Run("post", func(t *testing.T) {
		code, _, body := get(t, base+"/zh-CN/blog/free-search")
		if code != 200 {
			t.Fatalf("GET /zh-CN/blog/free-search: %d\n%s", code, body)
		}
		for _, want := range []string{
			`<h1 id="title">免费给模型加上联网搜索</h1>`,
			`<nav id="toc"><a href="#怎么装">怎么装</a><a href="#为什么免费">为什么免费</a></nav>`,
			`<h2 id="怎么装">怎么装</h2>`, `<h2 id="为什么免费">为什么免费</h2>`, `<h3 id="搜索从哪来">搜索从哪来</h3>`,
			`<td>问一个今天的问题</td>`,
			`<a href="/zh-CN/docs/install">文档</a>`, `<a href="https://example.org/search">官方说明</a>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the post lacks %s:\n%s", want, body)
			}
		}
		// The body is in the markup once: the hydration data has a marker in its place.
		if n := strings.Count(body, "搜索走公开接口"); n != 1 {
			t.Errorf("the page has the body %d times:\n%s", n, body)
		}
		if strings.Contains(body, "作者笔记") {
			t.Errorf("the post has the comment of the author:\n%s", body)
		}
		img := regexp.MustCompile(`<img src="(/assets/diagram-[A-Za-z0-9_-]+\.png)" alt="架构图" loading="lazy">`).FindStringSubmatch(body)
		if img == nil {
			t.Fatalf("the post has no image under /assets/ with a hash:\n%s", body)
		}
		code, header, png := get(t, base+img[1])
		want, err := os.ReadFile(filepath.Join(dir, "content", "blog", "zh-CN", "202608", "free-search", "diagram.png"))
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 || png != string(want) || header.Get("Content-Type") != "image/png" || !strings.Contains(header.Get("Cache-Control"), "immutable") {
			t.Errorf("GET %s: %d %q %q, %d bytes", img[1], code, header.Get("Content-Type"), header.Get("Cache-Control"), len(png))
		}

		code, _, body = get(t, base+"/en/blog/free-web-search")
		if code != 200 {
			t.Fatalf("GET /en/blog/free-web-search: %d\n%s", code, body)
		}
		for _, want := range []string{
			`<img src="` + img[1] + `" alt="Architecture" loading="lazy">`,
			`<nav id="toc"><a href="#how-to-install">How to install</a><a href="#how-to-install-1">How to install</a></nav>`,
			`<h2 id="how-to-install">How to install</h2>`, `<h2 id="how-to-install-1">How to install</h2>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the English post lacks %s:\n%s", want, body)
			}
		}
		// The bodies are read by the program and not served.
		if code, _, _ := get(t, base+"/content/blog/zh-CN/202608/free-search.html"); code != 404 {
			t.Errorf("GET a body file: %d", code)
		}
		if code, _, _ := get(t, base+"/zh-CN/blog/wip"); code != 404 {
			t.Errorf("GET the draft: %d", code)
		}
	})

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
		code, _, body = get(t, base+"/en/docs/guide/routing")
		if code != 200 || !strings.Contains(body, `<h1 id="doc">Routing</h1>`) || !strings.Contains(body, `<h2 id="parameters">Parameters</h2>`) ||
			!regexp.MustCompile(`<img src="/assets/tree-[A-Za-z0-9_-]+\.svg" alt="The folders of an app" loading="lazy">`).MatchString(body) {
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

// feedRoute is the only route of the content app without pages: a GET handler that writes the full
// text of a post, as a full-text feed does.
const feedRoute = `package feed

import (
	"io"

	"github.com/liustack/tifa"

	"example.com/contenttest"
)

func GET(c *tifa.Ctx) error {
	p, err := contenttest.Blog.Get("zh-CN", "202608/free-search")
	if err != nil {
		return err
	}
	w := c.Writer()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = io.WriteString(w, string(p.Body()))
	return err
}
`

// TestContentAppWithoutPages builds the content app with its pages taken out and a GET route in
// their place. The build still converts the Markdown with the plugins of vite.config.ts, and the
// image of the body is served under /assets/.
func TestContentAppWithoutPages(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "content")
	if err := os.RemoveAll(filepath.Join(dir, "app")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "app", "feed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "feed", "route.go"), []byte(feedRoute), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, _ := buildWithOutput(t, dir, os.Getenv("PATH"))
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "dist", "ssr.js")); !os.IsNotExist(err) {
		t.Errorf("an app without pages has an SSR bundle: %v", err)
	}
	base, _, _ := start(t, exe)
	code, _, body := get(t, base+"/feed")
	if code != 200 || !strings.Contains(body, `<h2 id="怎么装">怎么装</h2>`) || !strings.Contains(body, "搜索走公开接口") {
		t.Fatalf("GET /feed: %d\n%s", code, body)
	}
	img := regexp.MustCompile(`<img src="(/assets/diagram-[A-Za-z0-9_-]{8}\.png)" alt="架构图" loading="lazy">`).FindStringSubmatch(body)
	if img == nil {
		t.Fatalf("the body has no image under /assets/ with a hash and the attribute of the rehype plugin:\n%s", body)
	}
	want, err := os.ReadFile(filepath.Join(dir, "content", "blog", "zh-CN", "202608", "free-search", "diagram.png"))
	if err != nil {
		t.Fatal(err)
	}
	code, header, png := get(t, base+img[1])
	if code != 200 || png != string(want) || header.Get("Content-Type") != "image/png" || !strings.Contains(header.Get("Cache-Control"), "immutable") {
		t.Errorf("GET %s: %d %q %q, %d bytes", img[1], code, header.Get("Content-Type"), header.Get("Cache-Control"), len(png))
	}

	// Without vite.config.ts the build says what to add.
	if err := os.Remove(filepath.Join(dir, "vite.config.ts")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "tool", "tifa", "build")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "add vite.config.ts, which uses the tifa plugin") || !strings.Contains(string(out), "pnpm add -D vite unified@11") {
		t.Errorf("build without vite.config.ts: %v\n%s", err, out)
	}
}
