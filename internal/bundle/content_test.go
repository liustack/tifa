package bundle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

// Under tifa dev the plugin of the dev server converts, and the bodies load their files from it.
func TestDevConvertsTheContent(t *testing.T) {
	root, c := contentApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true, c); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a := readDist(t, root, "content/blog/a.html"); !strings.Contains(a, `<img src="/__tifa/vite/content/blog/a/x.png" alt="x">`) || !strings.Contains(a, `<hr class="mark">`) {
		t.Errorf("a.html:\n%s", a)
	}
	if got := readDist(t, root, "content.json"); !strings.Contains(got, `"headings":[{"depth":2,"id":"hello-b","text":"Hello b"}]`) {
		t.Errorf("content.json:\n%s", got)
	}
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true, nil); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, name := range []string{".tifa/dist/content", ".tifa/dist/content.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", name, err)
		}
	}
}
