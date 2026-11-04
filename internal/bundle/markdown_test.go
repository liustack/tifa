package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/js"
)

// converted is what convertContent of the tifa Vite plugin returned, or the message it failed with.
type converted struct {
	HTML     map[string]string
	Headings map[string][]content.Heading
	Warnings []string
	Error    string
}

// convertScript runs convertContent of the plugin in an app at root with the entries of
// .tifa/content/entries.json. setup is JavaScript that sets markdown, the option of tifa(). A file
// a relative URL leads to becomes asset:<its path in the app>.
const convertScript = `import fs from 'node:fs'
import path from 'node:path'
import { convertContent } from './plugin.mjs'
const root = process.cwd()
const real = fs.realpathSync(root)
const { entries } = JSON.parse(fs.readFileSync('.tifa/content/entries.json', 'utf8'))
let markdown
%s
try {
  const result = await convertContent(root, entries, {
    markdown,
    command: 'tifa build',
    asset: (file) => 'asset:' + path.relative(real, file).split(path.sep).join('/'),
  })
  process.stdout.write(JSON.stringify(result))
} catch (err) {
  process.stdout.write(JSON.stringify({ error: err.message }))
}
`

// markdownApp writes files into a new app root whose node_modules are those of the tests.
func markdownApp(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	if err := os.Symlink(testModules(t), filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

// convert runs convertContent on entries in the app at root, see convertScript.
func convert(t *testing.T, root string, entries []content.Markdown, setup string) converted {
	t.Helper()
	list, err := json.Marshal(content.Entries{Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{
		".tifa/content/entries.json": string(list),
		"plugin.mjs":                 js.VitePlugin,
		"convert.mjs":                strings.Replace(convertScript, "%s", setup, 1),
	})
	cmd := exec.Command("node", "convert.mjs")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, err.(*exec.ExitError).Stderr)
	}
	var c converted
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return c
}

// markdownEntry is an entry of collection blog to convert, whose Markdown is in the app at source.
// Its front matter is three lines.
func markdownEntry(lang, id, source, fallback, body string) content.Markdown {
	return content.Markdown{
		Collection: "blog", Lang: lang, ID: id, Source: source, Fallback: fallback,
		Out: content.BodyFile("blog", lang, id), Text: "\n\n\n" + body,
		Frontmatter: []byte(`{"title":"T"}`),
	}
}

// The default steps: GFM, HTML kept as written, comments removed, the ids GitHub gives headings
// (an id written in the HTML stays), and the headings with the ids they end up with. The plugins of
// the app run where they should: remark plugins on the Markdown, rehype plugins after the ids and
// before the URLs are rewritten, with the path and the data of the entry on the vfile.
func TestMarkdownStepsRunInOrder(t *testing.T) {
	root := markdownApp(t, map[string]string{"content/blog/en/a/cover.png": "png"})
	body := strings.Join([]string{
		"# Hello, World!",
		"",
		"| a | b |",
		"| - | - |",
		"| 1 | ~~2~~ |",
		"",
		"<!-- a note for the author -->",
		"",
		`<div class="box"><b>kept</b><!-- inside --></div>`,
		"",
		"## Hello, World!",
		"",
		`<h2 id="own">Own id</h2>`,
		"",
		"## 怎么装 `go`",
		"",
		"![c](./cover.png)",
		"",
		"See https://example.org.",
		"",
	}, "\n")
	c := convert(t, root, []content.Markdown{markdownEntry("en", "a", "content/blog/en/a/index.md", "", body)}, `
const remarkShout = () => (tree) => {
  tree.children.push({ type: 'paragraph', children: [{ type: 'text', value: 'from remark: ' + tree.children.some((n) => n.type === 'table') }] })
}
function rehypeSee() {
  return (tree, file) => {
    const ids = []
    const srcs = []
    let comments = 0
    const visit = (n) => {
      if (n.type === 'comment') comments++
      if (n.type === 'element' && /^h[1-6]$/.test(n.tagName)) ids.push(n.properties.id)
      if (n.type === 'element' && n.tagName === 'img') srcs.push(n.properties.src)
      if (n.type === 'element' && n.tagName === 'h1') n.properties.id = 'renamed'
      ;(n.children ?? []).forEach(visit)
    }
    visit(tree)
    const t = file.data.tifa
    tree.children.push({ type: 'element', tagName: 'p', properties: { id: 'seen' }, children: [{ type: 'text',
      value: [ids.join(','), srcs.join(','), comments, file.path.endsWith('/content/blog/en/a/index.md'), t.collection, t.lang, t.id, t.frontmatter.title].join(' | ') }] })
  }
}
markdown = { remarkPlugins: [remarkShout], rehypePlugins: [rehypeSee] }
`)
	if c.Error != "" {
		t.Fatal(c.Error)
	}
	html := c.HTML["content/blog/en/a.html"]
	for _, want := range []string{
		`<h1 id="renamed">Hello, World!</h1>`,
		`<table>`, `<del>2</del>`,
		`<div class="box"><b>kept</b></div>`,
		`<h2 id="hello-world-1">Hello, World!</h2>`,
		`<h2 id="own">Own id</h2>`,
		`<h2 id="怎么装-go">怎么装 <code>go</code></h2>`,
		`<img src="asset:content/blog/en/a/cover.png" alt="c">`,
		`<a href="https://example.org">https://example.org</a>`,
		`<p>from remark: true</p>`,
		`<p id="seen">hello-world,hello-world-1,own,怎么装-go | ./cover.png | 0 | true | blog | en | a | T</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the HTML lacks %s:\n%s", want, html)
		}
	}
	for _, gone := range []string{"note for the author", "inside", "<!--"} {
		if strings.Contains(html, gone) {
			t.Errorf("the HTML has %q:\n%s", gone, html)
		}
	}
	want := []content.Heading{
		{Depth: 1, ID: "renamed", Text: "Hello, World!"},
		{Depth: 2, ID: "hello-world-1", Text: "Hello, World!"},
		{Depth: 2, ID: "own", Text: "Own id"},
		{Depth: 2, ID: "怎么装-go", Text: "怎么装 go"},
	}
	if got := c.Headings["content/blog/en/a.html"]; !slices.Equal(got, want) {
		t.Errorf("headings\n%+v\nwant\n%+v", got, want)
	}
}

// The ids of headings are those rehype-slug gives, which follows GitHub.
func TestMarkdownHeadingIDsAreThoseOfRehypeSlug(t *testing.T) {
	root := markdownApp(t, nil)
	titles := []string{
		"Hello, World!", "Hello, World!", "hello world", "Hello World 1", "Hello-World", "  spaced  out  ",
		"C++ & Go", "Ünïcödé Ⓐ ½ ²", "日本語の見出し", "한국어 제목", "Привет мир", "emoji 🎉 time", "under_score",
		"`code` and *em*", "[link](https://x.y) text", "a.b/c:d", "---", "", "",
	}
	var body strings.Builder
	for _, s := range titles {
		body.WriteString("## " + s + "\n\n")
	}
	ours := convert(t, root, []content.Markdown{markdownEntry("en", "a", "content/blog/en/a.md", "", body.String())}, "")
	if ours.Error != "" {
		t.Fatal(ours.Error)
	}
	script := `import fs from 'node:fs'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import remarkRehype from 'remark-rehype'
import rehypeRaw from 'rehype-raw'
import rehypeSlug from 'rehype-slug'
const text = fs.readFileSync(0, 'utf8')
const p = unified().use(remarkParse).use(remarkGfm).use(remarkRehype, { allowDangerousHtml: true }).use(rehypeRaw).use(rehypeSlug)
const tree = await p.run(p.parse(text))
const ids = []
const visit = (n) => { if (n.type === 'element' && /^h[1-6]$/.test(n.tagName)) ids.push(n.properties.id); (n.children ?? []).forEach(visit) }
visit(tree)
process.stdout.write(JSON.stringify(ids))
`
	writeTree(t, root, map[string]string{"slug.mjs": script})
	cmd := exec.Command("node", "slug.mjs")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(body.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var want []string
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range ours.Headings["content/blog/en/a.html"] {
		got = append(got, h.ID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ids\n%q\nrehype-slug\n%q", got, want)
	}
}

// Relative URLs of img, a, video, audio and source become the URLs of their files, looked for next
// to the Markdown and then next to the version in the default language. Others stay as written.
func TestMarkdownRewritesRelativeURLs(t *testing.T) {
	root := markdownApp(t, map[string]string{
		"content/blog/zh-CN/a/diagram.png":   "zh",
		"content/blog/zh-CN/a/shared.png":    "zh",
		"content/blog/en/a/diagram.png":      "en",
		"content/blog/en/a/my file.pdf":      "pdf",
		"content/blog/en/a/_assets/clip.mp4": "mp4",
		"content/blog/en/a/poster.jpg":       "jpg",
		"content/blog/en/a/sound.mp3":        "mp3",
		"content/blog/en/a/clip.webm":        "webm",
	})
	body := strings.Join([]string{
		"![own](./diagram.png) ![shared](shared.png)",
		"",
		"[pdf](./my%20file.pdf#page=2) [page](/en/docs) [hash](#top) [site](https://example.org/x.png) [mail](mailto:a@b.c) [proto](//cdn.example.org/x.png)",
		"",
		`<video src="_assets/clip.mp4?t=1" poster="./poster.jpg"><source src="./clip.webm"></video>`,
		"",
		`<audio src="sound.mp3"></audio>`,
		"",
	}, "\n")
	c := convert(t, root, []content.Markdown{markdownEntry("en", "a", "content/blog/en/a/index.md", "content/blog/zh-CN/a", body)}, "")
	if c.Error != "" {
		t.Fatal(c.Error)
	}
	html := c.HTML["content/blog/en/a.html"]
	for _, want := range []string{
		`src="asset:content/blog/en/a/diagram.png"`,
		`src="asset:content/blog/zh-CN/a/shared.png"`,
		`href="asset:content/blog/en/a/my file.pdf#page=2"`,
		`href="/en/docs"`, `href="#top"`, `href="https://example.org/x.png"`, `href="mailto:a@b.c"`, `href="//cdn.example.org/x.png"`,
		`<video src="asset:content/blog/en/a/_assets/clip.mp4?t=1" poster="asset:content/blog/en/a/poster.jpg"><source src="asset:content/blog/en/a/clip.webm"></video>`,
		`<audio src="asset:content/blog/en/a/sound.mp3"></audio>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the HTML lacks %s:\n%s", want, html)
		}
	}
}

// A translation may sit at another depth than the version in the default language. The place next
// to the default language counts only when the file next to the entry is missing, so a URL that is
// right for the entry does not fail for leading out of the collection from the other place.
func TestMarkdownFallbackCountsOnlyForAMissingFile(t *testing.T) {
	root := markdownApp(t, map[string]string{
		"content/blog/shared.png":           "shared",
		"content/blog/zh-CN/deep/post/a.png": "a",
	})
	c := convert(t, root, []content.Markdown{
		// Deeper than its default language, content/blog/zh-CN/post.md.
		markdownEntry("en", "deep/post", "content/blog/en/deep/post/index.md", "content/blog/zh-CN", "![s](../../../shared.png)\n"),
		// Shallower than its default language, content/blog/zh-CN/deep/post/index.md.
		markdownEntry("fr", "post", "content/blog/fr/post.md", "content/blog/zh-CN/deep/post", "![s](../shared.png) ![a](a.png)\n"),
	}, "")
	if c.Error != "" {
		t.Fatal(c.Error)
	}
	if html := c.HTML["content/blog/en/deep/post.html"]; !strings.Contains(html, `src="asset:content/blog/shared.png"`) {
		t.Errorf("en:\n%s", html)
	}
	if html := c.HTML["content/blog/fr/post.html"]; !strings.Contains(html, `src="asset:content/blog/shared.png"`) || !strings.Contains(html, `src="asset:content/blog/zh-CN/deep/post/a.png"`) {
		t.Errorf("fr:\n%s", html)
	}
	// A missing file names the place next to the default language, which is outside the collection.
	c = convert(t, root, []content.Markdown{
		markdownEntry("en", "deep/post", "content/blog/en/deep/post/index.md", "content/blog/zh-CN", "![m](../../../missing.png)\n"),
	}, "")
	want := "content/blog/en/deep/post/index.md:4:1: image ../../../missing.png not found: looked at content/blog/missing.png " +
		"(next to the default language it leads to missing.png, which is outside content/blog)"
	if c.Error != want {
		t.Errorf("error\n%s\nwant\n%s", c.Error, want)
	}
}

// Every problem of every entry fails the conversion, at its line in the Markdown file, whose front
// matter takes the first three lines.
func TestMarkdownURLProblemsFailTheConversion(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{"secret.png": "secret"})
	root := markdownApp(t, map[string]string{
		"content/blog/zh-CN/b/index.md": "---\n---\n",
		"content/blog/en/a/folder/x":    "x",
		"content/notes/x.png":           "x",
	})
	if err := os.Symlink(filepath.Join(tmp, "secret.png"), filepath.Join(root, "content/blog/en/a/linked.png")); err != nil {
		t.Fatal(err)
	}
	c := convert(t, root, []content.Markdown{
		markdownEntry("en", "a", "content/blog/en/a/index.md", "content/blog/zh-CN/a", strings.Join([]string{
			"![cover](./cover.jpg)",
			"",
			"See [b](../../zh-CN/b/index.md) and [c](./c.md#part).",
			"",
			"![out](../../../notes/x.png) ![linked](./linked.png) [folder](./folder)",
		}, "\n")),
		markdownEntry("zh-CN", "b", "content/blog/zh-CN/b/index.md", "", "text\n\n![x](x.png)\n"),
	}, "")
	want := strings.Join([]string{
		"content/blog/en/a/index.md:4:1: image ./cover.jpg not found: looked at content/blog/en/a/cover.jpg and content/blog/zh-CN/a/cover.jpg",
		"content/blog/en/a/index.md:6:5: ../../zh-CN/b/index.md is a Markdown file and has no URL of its own: link to the page that shows it, such as /en/blog/b",
		"content/blog/en/a/index.md:6:37: ./c.md#part is a Markdown file and has no URL of its own: link to the page that shows it, such as /en/blog/c",
		"content/blog/en/a/index.md:8:1: image ../../../notes/x.png leads to content/notes/x.png, which is outside content/blog: a collection holds only its own files",
		"content/blog/en/a/index.md:8:30: image ./linked.png links to " + mustReal(t, filepath.Join(tmp, "secret.png")) + ", which is outside content/blog: a collection holds only its own files",
		"content/blog/en/a/index.md:8:54: link ./folder is a folder, which has no URL of its own",
		"content/blog/zh-CN/b/index.md:6:1: image x.png not found: looked at content/blog/zh-CN/b/x.png",
	}, "\n")
	if c.Error != want {
		t.Errorf("error\n%s\nwant\n%s", c.Error, want)
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// An error a plugin of the app throws names the plugin and the place it gives.
func TestMarkdownPluginErrorsNameThePlugin(t *testing.T) {
	root := markdownApp(t, nil)
	c := convert(t, root, []content.Markdown{markdownEntry("zh-CN", "a", "content/blog/zh-CN/a/index.md", "", "one\n\n![x](https://cdn.example.org/0f9c.jpg)\n")}, `
function rehypeContentImages() {
  return (tree, file) => {
    const visit = (n) => {
      if (n.tagName === 'img') file.fail(n.properties.src + ' 未登记在当前内容单元的 assets.yaml', n)
      ;(n.children ?? []).forEach(visit)
    }
    visit(tree)
  }
}
const remarkAsync = () => async () => { throw new Error('no network') }
markdown = { rehypePlugins: [[rehypeContentImages, { root: '.' }]] }
if (process.env.ASYNC) markdown = { remarkPlugins: [remarkAsync] }
`)
	if want := "content/blog/zh-CN/a/index.md:6:1: rehype plugin rehypeContentImages: https://cdn.example.org/0f9c.jpg 未登记在当前内容单元的 assets.yaml"; c.Error != want {
		t.Errorf("error\n%s\nwant\n%s", c.Error, want)
	}
	t.Setenv("ASYNC", "1")
	c = convert(t, root, []content.Markdown{markdownEntry("zh-CN", "a", "content/blog/zh-CN/a/index.md", "", "one\n")}, `
const remarkAsync = () => async () => { throw new Error('no network') }
markdown = { remarkPlugins: [remarkAsync] }
`)
	if want := "content/blog/zh-CN/a/index.md: remark plugin remarkAsync: no network"; c.Error != want {
		t.Errorf("error\n%s\nwant\n%s", c.Error, want)
	}
}

// The packages of unified are the app's own, of the major versions tifa is tested with.
func TestMarkdownNeedsThePackagesOfUnified(t *testing.T) {
	testModules(t)
	check := func(root string) string {
		t.Helper()
		writeTree(t, root, map[string]string{
			"plugin.mjs": js.VitePlugin,
			"load.mjs": `import { loadMarkdown } from './plugin.mjs'
try { await loadMarkdown(process.cwd(), 'tifa build'); process.stdout.write('loaded') } catch (err) { process.stdout.write(err.message) }
`,
		})
		cmd := exec.Command("node", "load.mjs")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("node: %v", err)
		}
		return string(out)
	}
	if got := check(markdownApp(t, nil)); got != "loaded" {
		t.Errorf("with the packages of the tests: %s", got)
	}
	none := t.TempDir()
	if got, want := check(none), "the app has content collections, so tifa build turns Markdown into HTML with unified: pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10"; got != want {
		t.Errorf("without packages:\n%s\nwant\n%s", got, want)
	}
	some := t.TempDir()
	pkg := func(name, version string) map[string]string {
		return map[string]string{"node_modules/" + name + "/package.json": `{"name":"` + name + `","version":"` + version + `","type":"module","exports":"./index.js"}`, "node_modules/" + name + "/index.js": "export default {}"}
	}
	for _, p := range []map[string]string{pkg("unified", "11.0.5"), pkg("remark-parse", "11.0.0"), pkg("remark-gfm", "5.0.0"), pkg("remark-rehype", "10.1.0"), pkg("rehype-stringify", "10.0.1")} {
		writeTree(t, some, p)
	}
	want := strings.Join([]string{
		"the app has content collections, so tifa build turns Markdown into HTML with unified: pnpm add -D rehype-raw@7",
		"remark-gfm 5.0.0 is not a version tifa is tested with: pnpm add -D remark-gfm@4",
		"remark-rehype 10.1.0 is not a version tifa is tested with: pnpm add -D remark-rehype@11",
	}, "\n")
	if got := check(some); got != want {
		t.Errorf("with missing and other versions:\n%s\nwant\n%s", got, want)
	}
}

// The name of an asset keeps only what go:embed takes in a file name.
func TestMarkdownAssetNamesSuitGoEmbed(t *testing.T) {
	for name, want := range map[string]string{
		"diagram.png":       "diagram.png",
		"架构 图 (1).png":      "架构_图__1_.png",
		"what's-new.jpg":    "what_s-new.jpg",
		"emoji🎉.svg":        "emoji_.svg",
		"under_score-1.gif": "under_score-1.gif",
	} {
		var got string
		callPlugin(t, "assetName", []any{name}, &got)
		if got != want {
			t.Errorf("assetName(%q) = %q, want %q", name, got, want)
		}
	}
}
