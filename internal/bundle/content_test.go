package bundle

import (
	"context"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/content"
)

// contentApp is devApp with a collection blog of two entries that show the same image, and a
// rehype plugin of the app in vite.config.ts.
func contentApp(t *testing.T) (string, *Content) {
	t.Helper()
	root := devApp(t)
	writeTree(t, root, map[string]string{
		".tifa/vite.js": "import { tifa } from './vite/plugin.js'\nexport default (options = {}) => tifa({ markdown: options.markdown })\n",
		"vite.config.ts": "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"const rehypeMark = () => (tree) => { tree.children.push({ type: 'element', tagName: 'hr', properties: { className: ['mark'] }, children: [] }) }\n" +
			"export default { plugins: [tifa({ markdown: { rehypePlugins: [rehypeMark] } }), react()] }\n",
		"content/blog/a/x.png": "png",
	})
	var md []content.Markdown
	f := content.File{}
	for _, id := range []string{"a", "b"} {
		out := content.BodyFile("blog", "", id)
		md = append(md, content.Markdown{Collection: "blog", ID: id, Source: "content/blog/" + id + ".md", Out: out,
			Text: "\n\n## Hello " + id + "\n\n![x](./a/x.png)\n", Frontmatter: []byte(`{}`)})
		f["blog"] = append(f["blog"], content.Record{ID: id, Slug: id, Path: id, Group: id, Source: "content/blog/" + id + ".md", File: out, Data: []byte(`{}`)})
	}
	writeTree(t, root, map[string]string{"content/blog/a.md": "---\n---" + md[0].Text, "content/blog/b.md": "---\n---" + md[1].Text})
	return root, &Content{File: f, Markdown: md}
}

// readBody reads the body of the entry id of the collection blog, from the file content.json names.
func readBody(t *testing.T, root, id string) string {
	t.Helper()
	return readDist(t, root, bodyFile(t, root, id))
}

// bodyFile is the file of the body of the entry id of the collection blog that content.json
// names, relative to .tifa/dist.
func bodyFile(t *testing.T, root, id string) string {
	t.Helper()
	var f content.File
	if err := json.Unmarshal([]byte(readDist(t, root, content.FileName)), &f); err != nil {
		t.Fatal(err)
	}
	for _, r := range f["blog"] {
		if r.ID == id {
			return r.File
		}
	}
	t.Fatalf("content.json has no entry %s", id)
	return ""
}

// folders lists the folders of .tifa/dist/content.
func folders(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".tifa", "dist", "content"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func readDist(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// tifa build converts the bodies during the browser build: an image becomes one asset under
// /assets/ with a hash, and content.json gets the headings of each body.
func TestAppConvertsTheContent(t *testing.T) {
	root, c := contentApp(t)
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true, c); err != nil {
		t.Fatalf("App: %v", err)
	}
	a := readDist(t, root, "content/blog/a.html")
	m := regexp.MustCompile(`^<h2 id="hello-a">Hello a</h2>\n<p><img src="(/assets/x-[A-Za-z0-9_-]+\.png)" alt="x"></p><hr class="mark">$`).FindStringSubmatch(a)
	if m == nil {
		t.Fatalf("a.html:\n%s", a)
	}
	if b := readDist(t, root, "content/blog/b.html"); !strings.Contains(b, `src="`+m[1]+`"`) {
		t.Errorf("b.html does not share the image of a.html:\n%s", b)
	}
	if got := readDist(t, root, strings.TrimPrefix(m[1], "/")); got != "png" {
		t.Errorf("%s = %q", m[1], got)
	}
	if got := readDist(t, root, "content.json"); !strings.Contains(got, `"file":"content/blog/a.html","headings":[{"depth":2,"id":"hello-a","text":"Hello a"}]`) {
		t.Errorf("content.json:\n%s", got)
	}
	// Without collections the plugin has nothing to convert.
	if _, err := App(n, root, filepath.Join(root, "public"), true, nil); err != nil {
		t.Fatalf("App: %v", err)
	}
	for _, name := range []string{".tifa/dist/content", ".tifa/dist/content.json", ".tifa/content"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", name, err)
		}
	}
}

// A change of a body alone gives another build ID, as the program then serves other HTML.
func TestAppIDFollowsTheBody(t *testing.T) {
	root, c := contentApp(t)
	n := NewNode(root, io.Discard)
	defer n.Close()
	before, err := App(n, root, filepath.Join(root, "public"), true, c)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	c.Markdown[1].Text += "\nmore\n"
	after, err := App(n, root, filepath.Join(root, "public"), true, c)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	if before == after {
		t.Error("the build ID stays the same")
	}
}

// An image that a body and a stylesheet both use is one file of the build.
func TestAppHasOneFileForAnImageOfABodyAndOfCSS(t *testing.T) {
	root, c := contentApp(t)
	// Larger than what Vite inlines into the stylesheet.
	image := strings.Repeat("png ", 2048)
	writeTree(t, root, map[string]string{
		"content/blog/a/x.png": image,
		"app/page/style.css":   ".cover { background: url(../../content/blog/a/x.png) }\n",
		".tifa/client.ts":      "import '../app/page/style.css'\nimport View from '../app/page/view'\nconsole.log(View)\n",
	})
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true, c); err != nil {
		t.Fatalf("App: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, ".tifa", "dist", "assets", "x-*.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("images %q", files)
	}
	url := "/assets/" + filepath.Base(files[0])
	if a := readDist(t, root, "content/blog/a.html"); !strings.Contains(a, `src="`+url+`"`) {
		t.Errorf("a.html does not use %s:\n%s", url, a)
	}
	css, err := filepath.Glob(filepath.Join(root, ".tifa", "dist", "assets", "*.css"))
	if err != nil || len(css) != 1 {
		t.Fatalf("stylesheets %q, %v", css, err)
	}
	if data, _ := os.ReadFile(css[0]); !strings.Contains(string(data), url) {
		t.Errorf("the stylesheet does not use %s:\n%s", url, data)
	}
}

// An app without pages, such as one with only a GET route that writes a feed, converts with the
// plugins of its vite.config.ts and bundles nothing. The files of the bodies go into dist/assets
// with a hash in their names, as Vite names them.
func TestAppConvertsTheContentWithoutPages(t *testing.T) {
	root, c := contentApp(t)
	writeTree(t, root, map[string]string{"package.json": `{"type":"module"}`})
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), false, c); err != nil {
		t.Fatalf("App: %v", err)
	}
	a := readDist(t, root, "content/blog/a.html")
	m := regexp.MustCompile(`^<h2 id="hello-a">Hello a</h2>\n<p><img src="(/assets/x-[A-Za-z0-9_-]{8}\.png)" alt="x"></p><hr class="mark">$`).FindStringSubmatch(a)
	if m == nil {
		t.Fatalf("a.html:\n%s", a)
	}
	if got := readDist(t, root, strings.TrimPrefix(m[1], "/")); got != "png" {
		t.Errorf("%s = %q", m[1], got)
	}
	if b := readDist(t, root, "content/blog/b.html"); !strings.Contains(b, `src="`+m[1]+`"`) {
		t.Errorf("b.html does not share the image of a.html:\n%s", b)
	}
	if got := readDist(t, root, "content.json"); !strings.Contains(got, `"headings":[{"depth":2,"id":"hello-a","text":"Hello a"}]`) {
		t.Errorf("content.json:\n%s", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".tifa", "dist"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"assets", "build.json", "content", "content.json"}; !slices.Equal(names, want) {
		t.Errorf("dist holds %q, want %q", names, want)
	}
}

// tifa dev converts the content of an app without pages the same way: no dev server of Vite runs
// for it, so the files of the bodies are in .tifa/dist/assets as in a build. A later Content
// converts only the entry that changed.
func TestDevConvertsTheContentWithoutPages(t *testing.T) {
	root, c := contentApp(t)
	writeTree(t, root, map[string]string{"package.json": `{"type":"module"}`})
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	before, err := d.Build(context.Background(), filepath.Join(root, "public"), false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n, err := d.Content(context.Background(), false, c, false, nil); err != nil || n != 2 {
		t.Fatalf("Content = %d, %v: want both entries converted", n, err)
	}
	a := readBody(t, root, "a")
	m := regexp.MustCompile(`<img src="(/assets/x-[A-Za-z0-9_-]{8}\.png)" alt="x">`).FindStringSubmatch(a)
	if m == nil || !strings.Contains(a, `<hr class="mark">`) {
		t.Fatalf("a.html:\n%s", a)
	}
	if got := readDist(t, root, strings.TrimPrefix(m[1], "/")); got != "png" {
		t.Errorf("%s = %q", m[1], got)
	}
	c.Markdown[0].Text += "\nmore\n"
	if n, err := d.Content(context.Background(), false, c, false, nil); err != nil || n != 1 {
		t.Fatalf("Content = %d, %v: want only a converted", n, err)
	}
	if a := readBody(t, root, "a"); !strings.Contains(a, "<p>more</p>") {
		t.Errorf("a.html:\n%s", a)
	}
	if got := readDist(t, root, "content.json"); !strings.Contains(got, `"headings":[{"depth":2,"id":"hello-b","text":"Hello b"}]`) {
		t.Errorf("content.json lost the headings of b:\n%s", got)
	}
	// The body changes under open pages, which keep their build.
	after, err := d.Build(context.Background(), filepath.Join(root, "public"), false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if before != after {
		t.Error("the build ID changed with the content")
	}
}

// Under tifa dev the plugin of the dev server converts, and the bodies load their files from it.
// Content converts again the entries whose Markdown changed and those next to a file that changed,
// and a failure leaves the content as it was.
func TestDevConvertsTheContent(t *testing.T) {
	root, c := contentApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n, err := d.Content(context.Background(), true, c, false, nil); err != nil || n != 2 {
		t.Fatalf("Content = %d, %v: want both entries converted", n, err)
	}
	if a := readBody(t, root, "a"); !strings.Contains(a, `<img src="/__tifa/vite/content/blog/a/x.png" alt="x">`) || !strings.Contains(a, `<hr class="mark">`) {
		t.Errorf("a.html:\n%s", a)
	}
	if got := readDist(t, root, "content.json"); !strings.Contains(got, `"headings":[{"depth":2,"id":"hello-b","text":"Hello b"}]`) {
		t.Errorf("content.json:\n%s", got)
	}
	if n, err := d.Content(context.Background(), true, c, false, nil); err != nil || n != 0 {
		t.Errorf("Content again = %d, %v: want nothing converted", n, err)
	}
	// The image is next to a.md, in content/blog: both entries are in that folder.
	if n, err := d.Content(context.Background(), true, c, false, []string{"content/blog/a/x.png", "content/blog/a.md"}); err != nil || n != 2 {
		t.Errorf("Content after the image changed = %d, %v: want both entries converted", n, err)
	}
	c.Markdown[1].Text += "\n![y](./a/missing.png)\n"
	if _, err := d.Content(context.Background(), true, c, false, nil); err == nil || !strings.Contains(err.Error(), "content/blog/b.md:7:1: image ./a/missing.png not found") {
		t.Errorf("Content with a missing image: %v", err)
	}
	if b := readBody(t, root, "b"); strings.Contains(b, "missing") {
		t.Errorf("the failed conversion wrote b.html:\n%s", b)
	}
	c.Markdown[1].Text = strings.Replace(c.Markdown[1].Text, "missing", "x", 1)
	if n, err := d.Content(context.Background(), true, c, false, nil); err != nil || n != 1 {
		t.Errorf("Content after the fix = %d, %v: want b converted", n, err)
	}
	if n, err := d.Content(context.Background(), true, nil, false, nil); err != nil || n != 0 {
		t.Fatalf("Content without collections = %d, %v", n, err)
	}
	for _, name := range []string{".tifa/dist/content", ".tifa/dist/content.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", name, err)
		}
	}
}

// Each conversion of tifa dev writes its bodies into a folder of its own, which content.json names,
// and links there the bodies it did not convert. The bodies the app loaded stay until it loads
// others: an entry that fails Validate keeps the app on the old content, which must show the old
// bodies. A conversion that the app did not load goes at the next one.
func TestDevKeepsTheBodiesTheAppLoaded(t *testing.T) {
	root, c := contentApp(t)
	writeTree(t, root, map[string]string{"package.json": `{"type":"module"}`})
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if _, err := d.Build(ctx, filepath.Join(root, "public"), false); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := d.Content(ctx, false, c, false, nil); err != nil {
		t.Fatal(err)
	}
	loadedA, loadedB := bodyFile(t, root, "a"), bodyFile(t, root, "b")
	if err := d.ContentLoaded(); err != nil {
		t.Fatal(err)
	}
	stat := func(name string) os.FileInfo {
		t.Helper()
		info, err := os.Stat(filepath.Join(root, ".tifa", "dist", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return info
	}

	// A new body of a: content.json names new files, the one of b the same as before.
	c.Markdown[0].Text += "\nfirst edit\n"
	if n, err := d.Content(ctx, false, c, false, nil); err != nil || n != 1 {
		t.Fatalf("Content = %d, %v: want a converted", n, err)
	}
	firstA, firstB := bodyFile(t, root, "a"), bodyFile(t, root, "b")
	if firstA == loadedA || firstB == loadedB {
		t.Fatalf("content.json names %s and %s again", firstA, firstB)
	}
	if !strings.Contains(readDist(t, root, firstA), "first edit") || strings.Contains(readDist(t, root, loadedA), "first edit") {
		t.Fatal("the new body of a is not in a file of its own")
	}
	if !os.SameFile(stat(firstB), stat(loadedB)) {
		t.Error("the body of b is not linked")
	}

	// The app did not load it. Another edit: the bodies the app has stay, the ones it never had go.
	c.Markdown[0].Text += "\nsecond edit\n"
	if _, err := d.Content(ctx, false, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if got := readDist(t, root, loadedA); strings.Contains(got, "edit") {
		t.Errorf("the body the app loaded changed:\n%s", got)
	}
	if len(folders(t, root)) != 2 {
		t.Errorf("dist/content holds %q, want the bodies the app loaded and the new ones", folders(t, root))
	}
	if _, err := os.Stat(filepath.Join(root, ".tifa", "dist", filepath.FromSlash(firstA))); !os.IsNotExist(err) {
		t.Errorf("%s is left: %v", firstA, err)
	}

	// A conversion that fails leaves no folder behind.
	c.Markdown[1].Text += "\n![y](./a/missing.png)\n"
	if _, err := d.Content(ctx, false, c, false, nil); err == nil {
		t.Fatal("Content with a missing image worked")
	}
	if len(folders(t, root)) != 2 {
		t.Errorf("dist/content holds %q after a failure", folders(t, root))
	}
	c.Markdown[1].Text = strings.Replace(c.Markdown[1].Text, "\n![y](./a/missing.png)\n", "", 1)

	// Once the app loads the new content, the old bodies go.
	if err := d.ContentLoaded(); err != nil {
		t.Fatal(err)
	}
	if len(folders(t, root)) != 1 {
		t.Errorf("dist/content holds %q, want only the bodies the app loaded", folders(t, root))
	}
	if got := readBody(t, root, "a"); !strings.Contains(got, "second edit") {
		t.Errorf("a:\n%s", got)
	}
}
