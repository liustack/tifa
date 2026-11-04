package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestDevContent runs tifa dev on a copy of test/apps/content and changes its Markdown: each
// change runs the content step alone, without go build, and a reload shows it. The list page has
// a whole-page cache, which a reload of the content expires.
func TestDevContent(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	dir := copyApp(t, "content")
	d := startDev(t, dir)
	out := d.waitLog(t, 0, ready)
	expectSteps(t, "first build", out, "static analysis", "code generation", "bundle", "content", "go build")
	v := d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" }).Version

	// tifa dev shows drafts and posts whose date is still to come.
	expectBody(t, d.base+"/zh-CN/blog", "写了一半的草稿")
	expectBody(t, d.base+"/zh-CN/blog", "<span>draft</span>")
	expectBody(t, d.base+"/zh-CN/blog", "新版本发布")
	expectBody(t, d.base+"/zh-CN/blog/wip", "DRAFT-TEXT-MUST-NOT-SHIP")
	expectBody(t, d.base+"/zh-CN/blog/free-search", `<h1 id="title">免费给模型加上联网搜索</h1>`)

	// change edits the app, waits for the open pages to get a new version and checks that only
	// the content step ran.
	change := func(name string, edit func()) {
		t.Helper()
		mark := len(d.log.String())
		edit()
		v = d.waitStatus(t, func(st devStatus) bool { return st.Version != v && st.Error == "" }).Version
		expectSteps(t, name, d.waitLog(t, mark, ready), "content")
	}
	post := "content/blog/zh-CN/202608/free-search/index.md"

	// A new title in the front matter: the article and the cached list show it.
	change("front matter", func() { d.edit(t, post, "title: 免费给模型加上联网搜索", "title: 免费联网搜索") })
	expectBody(t, d.base+"/zh-CN/blog/free-search", `<h1 id="title">免费联网搜索</h1>`)
	expectBody(t, d.base+"/zh-CN/blog", "免费联网搜索")

	// A new post adds an item to the list.
	change("new post", func() {
		write(t, filepath.Join(dir, "content/blog/en/202612/winter/index.md"), "---\ntitle: Winter notes\ndescription: Cold\ndate: 2026-10-30\n---\n\nSnow.\n")
	})
	expectBody(t, d.base+"/en/blog", "Winter notes")
	expectBody(t, d.base+"/en/blog/winter", "<p>Snow.</p>")

	// A broken front matter shows the message of tifa build, and the old content keeps serving.
	mark := d.edit(t, post, "tags: [DeepSeek, 联网搜索]", "tag: [DeepSeek]")
	failed := d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if failed.Title != "Build failed" || !strings.Contains(failed.Error, post+`:6:1: "tag" is not a field of contenttest.Post. Did you mean "tags"?`) {
		t.Fatalf("overlay %q: %s", failed.Title, failed.Error)
	}
	if names, _ := steps(d.log.String()[mark:]); len(names) > 0 {
		t.Fatalf("a broken front matter ran %v", names)
	}
	expectBody(t, d.base+"/zh-CN/blog/free-search", `<h1 id="title">免费联网搜索</h1>`)
	change("fixed front matter", func() { d.edit(t, post, "tag: [DeepSeek]", "tags: [DeepSeek, 联网搜索]") })

	// An entry that fails Validate, in the same edit as its body: the app keeps the content it
	// has, with the old body.
	{
		data, err := os.ReadFile(filepath.Join(dir, post))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(data), "cover: https://static.example.com/", "cover: https://elsewhere.example.com/", 1)
		write(t, filepath.Join(dir, post), strings.Replace(text, "模型推理漂亮", "新的开头，模型推理漂亮", 1))
	}
	failed = d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if failed.Title != "The app did not load the content" || !strings.Contains(failed.Error, post+": Validate: cover must start with https://static.example.com/") {
		t.Fatalf("overlay %q: %s", failed.Title, failed.Error)
	}
	expectBody(t, d.base+"/zh-CN/blog/free-search", `<h1 id="title">免费联网搜索</h1>`)
	if _, _, body := get(t, d.base+"/zh-CN/blog/free-search"); strings.Contains(body, "新的开头") || !strings.Contains(body, "<p>模型推理漂亮") {
		t.Fatalf("the page shows another body than the content the app kept:\n%.800s", body)
	}
	change("valid again", func() { d.edit(t, post, "cover: https://elsewhere.example.com/", "cover: https://static.example.com/") })
	expectBody(t, d.base+"/zh-CN/blog/free-search", "<p>新的开头，模型推理漂亮")

	// The image of a page bundle, which the English version falls back to: a new one shows on
	// both pages, a missing one fails both.
	img := regexp.MustCompile(`<img src="(/__tifa/vite/content/blog/zh-CN/202608/free-search/diagram\.png)"`)
	_, _, body := get(t, d.base+"/en/blog/free-web-search")
	m := img.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the English post has no image from the Chinese one:\n%.600s", body)
	}
	image := filepath.Join(dir, "content/blog/zh-CN/202608/free-search/diagram.png")
	if err := os.Remove(image); err != nil {
		t.Fatal(err)
	}
	failed = d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if !strings.Contains(failed.Error, "content/blog/en/202608/free-search/index.md:11:1: image ./diagram.png not found") ||
		!strings.Contains(failed.Error, "content/blog/zh-CN/202608/free-search/index.md:12:1: image ./diagram.png not found") {
		t.Fatalf("overlay %q: %s", failed.Title, failed.Error)
	}
	change("new image", func() { write(t, image, "new image") })
	expectBody(t, d.base+m[1], "new image")
	expectBody(t, d.base+"/zh-CN/blog/free-search", m[1])

	// content.go is Go: a new schema field changes the types of the pages, so every step runs and
	// every entry is converted again.
	mark = d.edit(t, "content.go", "Order int    `json:\"order,omitempty\"`", "Order int    `json:\"order,omitempty\"`\n\tNote  string `json:\"note,omitempty\"`")
	d.waitStatus(t, func(st devStatus) bool { return st.Version != v && st.Error == "" })
	out = d.waitLog(t, mark, ready)
	expectSteps(t, "content.go", out, "static analysis", "code generation", "bundle", "content", "go build")
	if !strings.Contains(out, "entries converted") || strings.Contains(out, "(0 entries converted)") {
		t.Fatalf("content.go did not convert every entry:\n%s", out)
	}
	expectBody(t, d.base+"/zh-CN/blog/free-search", `<h1 id="title">免费联网搜索</h1>`)
}
