---
summary: '应用怎样把文字作为 Markdown 放在 content/ 下：content.go、front matter、slug、草稿、remark'
audience: app
order: 9
translatedFrom: c5c6a26c89c4
read_when:
  - '给 Tifa 应用加博客、文档或其他 Markdown 内容'
  - '编写或修改 content.go，或者嵌入 content.Entry 的 schema 类型'
  - '构建报出关于 content.Define、content/ 下某个文件夹或某个文件 front matter 的错误'
  - '用 Body 和 <HTML> 显示条目正文，用 Headings 做目录，或者在 vite.config.ts 的 tifa() 里加 remark 或 rehype 插件'
  - '正文里的图片或链接找不到，或者某个插件让转换失败'
  - '按日期在指定时间发布条目，或者条目到了日期却没有出现'
  - '用 tifatest.LoadContent、tifatest.SetContent 或 tifatest.SetContentTime 写读取集合的 Go 测试'
  - '修改 content 包、internal/content、internal/analyze 里的内容检查，或者 js/vite/plugin.js 里的 Markdown 步骤'
---

# 内容 {#content}

集合（collection）是 `content/` 下的一个 Markdown 文件夹，应用在 Go 里把它读成有类型的值：博客、一套文档、发布说明。构建会读取这些文件，对照一个 Go 结构体检查每份 front matter，把每篇正文转成 HTML，再把结果放进程序里。运行时，`Load` 或处理函数像读任何其他数据一样读取这些条目，不再做任何解析。

**适用范围。** 集合适合每个应用大约 1000 篇以内的条目。1000 篇、每篇约 10 KB 时，程序会增大约 10 MB，构建转换它们大约需要 2 秒。正文用到的每张图片也会进入程序，所以大图或很多图片请放在对象存储里，用 URL 链接。超过 1000 篇，或者站点运行期间还有人在编辑的文字，请放进数据库，并用 Tifa 做编辑页面，就像一个小型 CMS。

四步把一个条目放到页面上：

1. 在 `content.go` 里声明集合（这个结构体就是 front matter 的 schema）。
2. 把条目写成 `content/` 下的 Markdown 文件。
3. 在 `Load` 里用集合的方法读取它们。
4. 用 `<HTML>` 显示正文。

本页的代码来自 [test/apps/content](../../test/apps/content)，一个有两种语言的博客和文档的应用。

## 声明集合 {#declaring-a-collection}

集合在应用根目录的 `content.go` 里声明：

```go
// content.go
package contenttest

import (
	"fmt"
	"strings"
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs", Languages: []string{"zh-CN", "en"}})
)

type Post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}

type Doc struct {
	content.Entry
	Title string `json:"title"`
	Order int    `json:"order,omitempty"` // the place in the side bar, smaller first, then by ID
}
```

| `Source` 字段 | 含义 |
| --- | --- |
| `Dir` | 必填。`content/` 下存放这个集合的文件夹，也是它在错误信息和构建里的名字。只能用字母、数字、`-` 和 `_`，不能有 `/`。 |
| `Languages` | 可选。集合的语言，默认语言排第一：用 `tifa.Languages()` 取站点的语言，或者写一个字符串字面量的列表。设置了它之后，`content/<Dir>/` 下的第一层，每种语言一个文件夹。 |
| `Match` | 可选。一个只保留部分文件的模式，见[哪些文件是条目](#which-files-are-entries)。 |

构建读取 `content.go` 时不会运行它，所以：

- `content.Define` 只能写成 `content.go` 里某个包级 `var` 的值。写在别处（另一个文件、函数、`init()`、赋值语句）会让构建失败。
- 参数是就地写出的 `content.Source{...}` 字面量，字段要写上名字。`Dir` 和 `Match` 是字符串字面量，`Languages` 是 `tifa.Languages()` 或由字符串字面量组成的 `[]string{...}`，不能是常量或变量。
- 类型是应用模块里的一个命名结构体，并且嵌入 `content.Entry`（不是指向它的指针）。
- 两个集合不能读同一个 `Dir`。
- `content/` 下的每个文件夹都需要一个集合，没有 `content.go` 的应用也一样。没有任何集合读取的文件夹会让构建失败，这样就能抓出文件夹名里的拼写错误。名字以 `.` 或 `_` 开头的文件夹不管。

根包不会 import `app/` 下的包（见[结构](../start/structure.zh-CN.md)），所以 schema 类型放在 `content.go` 里，或者放在一个不 import 任何业务域的共享包里。集合的查询（`Published`、`BySlug` 之类）放在业务域的包里，它读 `contenttest.Blog` 的方式，和读 `contenttest.DB` 一样。

## 哪些文件是条目 {#which-files-are-entries}

```text
content/
  blog/
    zh-CN/
      202608/free-search/index.md     an entry (a page bundle)
      202608/free-search/diagram.png  belongs to it
      202608/free-search/wechat.md    belongs to it, not an entry
      202611/launch/index.md
    en/
      202608/free-search/index.md
  docs/
    zh-CN/index.md  install.md
    en/
      index.md  install.md            one entry each (loose files)
      guide.md                        the front page of guide/
      guide/routing.md                an entry of its own
      guide/tree.svg                  not an entry
```

按这个顺序判断：

1. 名字以 `.` 或 `_` 开头的文件和文件夹会被跳过。
2. 含有 `index.md` 的文件夹是一个条目，叫 page bundle（页面包）。它下面的一切，其他 `.md` 文件和子文件夹也一样，都属于这个条目。集合文件夹和语言文件夹不是 page bundle：那里的 `index.md` 是一个独立文件，ID 是 `index`。
3. 其他每个 `.md` 文件自己是一个条目，叫独立文件（loose file）。
4. 设置了 `Match` 时，只有满足上面两条规则、并且匹配它的文件才是条目。模式从语言文件夹下面开始（没有语言时从集合文件夹下面开始），各段用 `/` 分隔，写法和 `path.Match` 一样，`**` 匹配任意多层文件夹。`Match: "*/*/index.md"` 只保留两层深的 page bundle，不要独立文件。
5. `.mdx` 文件会让构建失败。其他扩展名的文件不是条目。

符号链接可以指向集合文件夹之内的文件或文件夹。指向它之外的链接，或指向包含这个链接的文件夹的链接，会让构建失败，这样就不会有别处的文件跑进程序里。page bundle 里的图片和其他文件也一样。

一组文档的首页，请写一个和文件夹同名的文件（`guide/` 旁边放 `guide.md`）。`guide/index.md` 会让整个 `guide/` 成为一个 page bundle，两者都有的话，构建会以重复的 ID 失败。

## ID、slug、路径和语言 {#id-slug-path-and-languages}

- **ID**：文件在集合文件夹和语言文件夹之下的路径，去掉扩展名和结尾的 `/index`。`content/blog/zh-CN/202608/free-search/index.md` 的 ID 是 `202608/free-search`。front matter 不能改它。集合的一种语言里每个 ID 只出现一次。
- **Slug**：front matter 里的 `slug`，或者 ID 的最后一段。它必须是 URL 的一段：不能为空，不能是 `.` 或 `..`，不能有 `/`、`?`、`#`、`%`、空格或控制字符。其他语言的文字是可以的。
- **路径**：把 ID 的最后一段换成 slug 之后的结果，没有 `slug` 时就是 ID 本身。集合的一种语言里每个路径只出现一次。slug 可以重复（`guide/install` 和 `api/install` 的 slug 都是 `install`）。URL 用 slug 的博客，会在测试里检查它的 slug 不重复，就像 `app/blog/post_test.go` 里的 `TestSlugsAreUnique` 那样。

设置了 `Languages` 时，每个条目都在某个语言文件夹里，默认语言也一样。第一层里不是语言的文件夹，或者直接放在集合文件夹里的 `.md` 文件，会让构建失败。没有文件夹的语言就没有条目。没有 `Languages` 时没有语言这一层，`Entry.Language` 是 `""`。

同一个条目在不同语言里的各个版本共用一个翻译键：front matter 里的 `translationKey`，或者 ID。所以路径相同的文件互为翻译，而 `translationKey` 把路径不同的文件连起来，比如测试应用里的 `launch`。一种语言里每个键只出现一次。`Entry.Translations` 列出现在能看到的版本，按 `Languages` 的顺序，包括条目自己，每个写成 `{Language, ID, Slug, Path}`。语言之间的链接和 `hreflang` 就来自这里。

在有多语言的站点上（见[多语言站点](../guides/i18n.zh-CN.md)），写 `Languages: tifa.Languages()`，这个集合就有站点的语言，默认语言排第一。`content/<Dir>/` 下的文件夹就以它们命名。某个语言里没有版本的条目，在那个语言里就没有页面：对那个语言，`Get` 返回 `tifa.ErrNotFound`，所以返回这个错误的 `Load` 会显示 404，也不会回退到别的语言。条目的页面把 `entry.Translations` 传给 `c.SetAlternates`，这样 `hreflang` 链接只列出存在的版本。

某个语言里没有版本的条目是没问题的。构建会为每个集合和每个缺失的语言打印一行：

```text
tifa: content: 2 entries of docs have no zh-CN version: guide, guide/routing (warning)
```

## Front matter {#front-matter}

每个条目都以两行 `---` 之间的 YAML front matter 开头：

```markdown
---
title: Free web search for your model
description: One free skill pack lets the model search the web and cite every answer.
date: 2026-08-02
slug: free-web-search
tags: [search]
---
```

它的键是 schema 结构体各字段的 `json` 名，构建按和 `Load` 结果、处理函数输入同样的 JSON 规则来检查它们（[JSON 规则](../reference/json.zh-CN.md)）。

- 字段可以是字符串、数字、布尔值、`time.Time`、指针、切片、键为字符串的 map，或嵌套结构体。`time.Time` 接受 `2006-01-02`（UTC 午夜）和 RFC 3339，加不加引号都行。自己读取 JSON 的类型、`[]byte`、`time.Duration`、接口，以及除 `omitempty` 和 `omitzero` 之外的 json 选项（比如 `,string`），都会让构建失败。
- 字段是必填的，除非它是指针、切片或 map，或者带 `omitempty` 或 `omitzero`。这和页面的 TypeScript 类型里可选字段的规则一样。
- 不是字段的键只是一个警告，附近有相近的字段名时，会给出最接近的那个，这个条目读取时不带它。所以从 Hugo 或 Jekyll 搬来的页面，front matter 里带着 `weight` 或 `categories`，也能构建。想写成某个字段却拼错了的键，也会在警告里看到。
- 字段上有 `yaml` 标签会让构建失败：键就是 json 名。
- Tifa 自己读取四个键。它们不是 schema 的字段，写上它们不算错误：

| 键 | 含义 |
| --- | --- |
| `date` | 条目的发布时间，即 `Entry.Date`。只写日期（`2026-11-20`）表示 UTC 午夜。要给确切的时间，请写带时区的 RFC 3339，如 `2026-11-20T09:00:00+08:00`。 |
| `draft` | `true` 表示草稿。 |
| `slug` | slug，见上文。 |
| `translationKey` | 把一个条目在不同语言里的各个版本连起来。 |

- `id`、`language`、`path`、`translations` 和 `words` 由 Tifa 设置，写上它们会让构建失败。schema 字段的 json 名如果和 `content.Entry` 的某个 json 名，或和名字 `translationKey` 相同，也会让构建失败。

`content.Entry` 里放着 Tifa 设置的内容：

```go
type Entry struct {
	ID           string        `json:"id"`
	Language     string        `json:"language"`
	Slug         string        `json:"slug"`
	Path         string        `json:"path"`
	Date         time.Time     `json:"date,omitzero"`
	Draft        bool          `json:"draft,omitzero"`
	Translations []Translation `json:"translations"`
	Words        int           `json:"words"`
}
```

`Words` 统计正文的字数：每个中文、日文或韩文字符算一个词，其他字母和数字的每一段连续字符也算一个词。代码块、HTML 注释和标签、链接地址都不计入。它是在 Markdown 上统计的，所以不需要转换。阅读时间是应用自己的算式，比如 `Math.max(1, Math.ceil(words / 300))` 分钟。

front matter 里的 URL，比如 `cover`，只是普通字符串。它们不会被改写，这一点和正文里的图片不同。

### 构建检查不了的规则：Validate {#rules-the-build-cannot-check-validate}

构建不会运行应用的代码。像允许值的列表或 URL 前缀这样的规则，要写在 schema 的 `Validate() error` 方法里，同样放在 `content.go` 中：

```go
const assetOrigin = "https://static.example.com/"

func (p Post) Validate() error {
	if p.Date.IsZero() {
		return fmt.Errorf("date: a post needs one")
	}
	if p.Cover != "" && !strings.HasPrefix(p.Cover, assetOrigin) {
		return fmt.Errorf("cover must start with %s", assetOrigin)
	}
	return nil
}
```

它在程序启动时、`OnStart` 之前，对每个条目运行。任何一个失败时，程序打印出来，并且不监听就退出：

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
```

`tifatest.LoadContent` 也会运行它，所以 `go test` 以及 `tifa check` 的测试步骤，会在部署之前发现问题。

## 读取集合 {#reading-a-collection}

| `*content.Collection[T]` 的方法 | 返回 |
| --- | --- |
| `All() []T` | 现在能看到的所有语言的条目，按 ID 排序，同一个 ID 内按 `Languages` 的顺序。每次调用返回一个新的切片，调用者可以随意排序或修改。 |
| `In(language string) []T` | 某一种语言的条目，按 ID 排序。集合没有的语言返回 `nil`。对没有语言的集合，`In("")` 就是 `All()`。 |
| `Get(language, id string) (T, error)` | 一个条目。没有它或它还不能被看到时返回 `tifa.ErrNotFound`，所以 `Load` 可以原样返回这个错误。对没有语言的集合，`language` 是 `""`，传别的会 panic。 |
| `Languages() []string` | 声明的语言，默认语言排第一。 |

按日期或 `order` 字段排序，按 slug 或标签查找，在业务域里就是几行 Go（`app/blog/post.go`）：

```go
// Published is the posts of a language that can be seen now, newest first.
func Published(language string) []contenttest.Post {
	list := contenttest.Blog.In(language) // sorted by ID, drafts and posts still to come left out
	slices.SortStableFunc(list, func(a, b contenttest.Post) int { return b.Date.Compare(a.Date) })
	return list
}

func BySlug(language, slug string) (contenttest.Post, error) {
	for _, p := range contenttest.Blog.In(language) {
		if p.Slug == slug {
			return p, nil
		}
	}
	return contenttest.Post{}, tifa.ErrNotFound
}
```

内容在 `OnStart` 之前就加载好了，所以 `OnStart` 可以读它。在包初始化期间读取集合会 panic。能看到的条目会在某个日期到来时变化，在 `tifa dev` 下，内容在应用运行期间也会变。所以，依赖内容的东西，请在需要它的函数里计算（比如 RSS 处理函数），不要在 `OnStart` 里留一份副本。

## 显示正文 {#showing-the-body}

`Body()` 和 `Headings()` 是 `content.Entry` 的方法，不是字段，所以页面数据里的条目列表不会带任何正文。文章页自己把它们放进自己的数据里（`app/blog/post+/route.go`）：

```go
type Page struct {
	Post     contenttest.Post  `json:"post"`
	Body     tifa.HTML         `json:"body"`
	Headings []content.Heading `json:"headings"`
}

func Load(c *tifa.Ctx) (Page, error) {
	p, err := blog.BySlug(c.Language(), c.Param("post"))
	if err != nil {
		return Page{}, err
	}
	return Page{Post: p, Body: p.Body(), Headings: p.Headings()}, nil
}
```

```tsx
// page.tsx
import { HTML } from 'tifa/client'
import { page, type PageData } from './route.gen'

export default function PostPage() {
  const { post, body, headings } = page.useLoaderData()
  return (
    <article>
      <h1 id="title">{post.title}</h1>
      <nav id="toc">
        {headings
          .filter((h) => h.depth === 2)
          .map((h) => (
            <a key={h.id} href={`#${h.id}`}>
              {h.text}
            </a>
          ))}
      </nav>
      <HTML value={body} id="body" className="prose" />
    </article>
  )
}
```

- `Body() tifa.HTML` 每次被调用时，都从程序里读取条目的 HTML（`tifa dev` 从 `.tifa/dist` 读取），不保留副本。页面用 `<HTML>` 显示它，见[显示应用信任的 HTML](loading-data.zh-CN.md#showing-html-the-app-trusts)。正文里的链接走路由器。
- `Headings() []content.Heading` 按顺序列出正文的 `h1` 到 `h6`，每个写成 `{Depth, ID, Text}`：它的级别、它在 HTML 里的 `id` 和它的文字。

## Markdown 转 HTML {#markdown-to-html}

`tifa build` 用 [unified](https://unifiedjs.com/)，在 `vite.config.ts` 里的 Tifa 插件中，把每个条目的 Markdown 转成 HTML。有集合的应用要安装这些包，用这些大版本：

```sh
pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
```

缺了某个包，或者大版本不对，构建会失败，并给出安装它的命令。Tifa 只在有集合的应用里才加载它们。

转换在 Vite 里运行，所以即使没有页面，有条目的应用也需要 `package.json` 和带 Tifa 插件的 `vite.config.ts`，比如只有一个写 `Body()` 的 feed 或 API 的应用。没有页面时，构建用 `vite.config.ts` 的插件做转换，不打包任何东西。`tifa check` 读取 front matter，不需要 Node。

每个条目按这个顺序经过这些步骤：

1. front matter 变成同样数量的空行，所以错误里的行号就是文件里的行号。
2. `remark-parse` 和 `remark-gfm`（表格、删除线、任务列表、自动链接）。
3. 应用的 `remarkPlugins`。
4. `remark-rehype` 和 `rehype-raw`：Markdown 里写的 HTML 保持原样。内容是仓库的一部分，是受信任的，所以不做任何过滤。
5. 移除 HTML 注释，这样写给作者的备注不会出现在页面上。
6. 每个 `h1` 到 `h6` 都会得到 `id`。标题末尾的 `{#id}` 就是它的 `id`，并且不留在文字里，见[标题 id](#heading-ids)。没有写的标题，得到 GitHub 给它的那个 id，和 `rehype-slug` 一样：文字转小写并去掉标点，空格变成 `-`，重复的依次加 `-1`、`-2` 等等。HTML 里写了 `id` 的标题保持原样。
7. 应用的 `rehypePlugins`。它们在 id 之后运行，所以像 `rehype-autolink-headings` 这样的插件可以用到这些 id。
8. 改写相对 URL，见[图片和链接](#images-and-links)。
9. 以根路径开头的链接加上语言前缀，见[语言之间的链接](#links-between-languages)。
10. 收集标题给 `Headings()`，用它们最终得到的 id。
11. `rehype-stringify`。

应用的插件放进 `vite.config.ts` 里的 `tifa()`，每个都按 unified 接受的方式来写，一个插件，或 `[plugin, options]`。测试应用加了一个自己的 rehype 插件（`markdown.ts`），让图片在进入视口时才加载：

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'
import { rehypeLazyImages } from './markdown'

export default defineConfig({
  plugins: [tifa({ markdown: { rehypePlugins: [rehypeLazyImages] } }), react()],
})
```

```ts
// markdown.ts
type Node = { type: string; tagName?: string; properties?: Record<string, unknown>; children?: Node[] }

export function rehypeLazyImages() {
  const visit = (node: Node) => {
    if (node.type === 'element' && node.tagName === 'img') node.properties = { ...node.properties, loading: 'lazy' }
    node.children?.forEach(visit)
  }
  return visit
}
```

- 用 `@shikijs/rehype` 做代码高亮时，除非你传了 `langs`，否则它会在第一个代码块之前加载 Shiki 的全部 230 种语言。对 35 篇文档来说，这占了 2.6 秒转换时间里的 1.8 秒。请列出你的文档用到的语言：`[rehypeShiki, { themes: { light: 'github-light', dark: 'github-dark' }, langs: ['go', 'tsx', 'sh'] }]`。
- Tifa 自己的步骤不能关掉。要撤销其中一步，比如 GFM 把裸 URL 变成链接，请加一个你自己的插件。
- 每个条目的 vfile 有 `file.path`（Markdown 文件的完整路径）和 `file.data.tifa`，后者是 `{ collection, language, id, frontmatter }`，其中的 front matter 是按 schema 转成的 JSON。要读取条目旁边某个文件的插件，可以从 `file.path` 找到它。
- 插件抛出的错误会让构建失败，并给出文件、插件给出的位置和插件的名字。它添加的警告会被打印出来。
- 不支持 MDX：正文是 HTML，不能包含 React 组件。请把组件放在页面里。

### 标题 id {#heading-ids}

把 id 写在标题末尾，和 Hugo、Docusaurus、VitePress、Pandoc 的写法一样：

```md
## 图片和链接 {#images-and-links}
```

这个标题得到 `id="images-and-links"`，`{#images-and-links}` 和它前面的空格不留在文字里，所以 `Headings()` 返回这个 id 和文字 `图片和链接`。译文保留原文的 id，指向它的链接和目录就都还能用。

- 只有标题最末尾的 `{#id}` 算数。id 可以包含字母（任何语言）、数字、`-`、`_`、`.` 和 `:`。其他的，比如 `{#bad id}`，仍是文字，行内代码里或文字中间的 `{#id}` 也是。
- 这样写的 id 先分配。没有写的标题随后得到它的 GitHub id，所以一个会和页面下方某个已写 id 重复的自动 id 会得到 `-1`、`-2` 等等，已写的 id 保持原名。
- 同样的规则也适用于两个已写的 id 重复：第二个得到 `-1`。
- 用 HTML 写、带 `id` 的标题保持原样，文字和 id 都不动，它的 id 也不计入重复。

### 图片和链接 {#images-and-links}

正文里的相对 URL（不以 `/`、`#` 或 `https:` 这样的协议开头）会在这些地方被改写：`img`、`video`、`audio` 和 `source` 的 `src`，`video` 的 `poster`，以及 `a` 的 `href`。

- 文件先在 Markdown 文件旁边找。找不到时，再到这个条目默认语言版本的旁边找，所以一张图片只需在默认语言旁边保存一份，需要另一张图片的翻译，就在它旁边放自己的同名文件。
- 文件会随浏览器包一起发出，名字是 `/assets/<name>-<hash>.<ext>`，和其他资源一样带长期缓存。被多个条目用到的文件只有一份。在 `tifa dev` 下，URL 通过 Vite 的开发服务器指向这个文件。
- 文件必须在集合的文件夹之内：带 `..` 的 URL，或指向它之外的符号链接，会让构建失败。
- 查询或 `#fragment` 会保留在新的 URL 上。
- 指向 `.md` 文件的相对链接会让构建失败。Markdown 文件没有自己的 URL：页面的 URL 由应用的路由给出，所以请链接到那里，比如 `/zh-CN/docs/install`。
- 以根路径开头的链接（`/store/cart`）保持不动，除非站点有多语言（见下文）。
- 本地图片不会加 `width`、`height` 或 `srcset`。需要时，请用你自己的 rehype 插件加上。

### 语言之间的链接 {#links-between-languages}

在有多语言的站点上，带语言的条目里以根路径开头的链接，比如 `[cart](/store/cart)`，当它指向的页面有那种语言时，会加上那种语言的前缀：在 `en-US` 条目里就是 `/en-US/store/cart`。默认语言不加前缀，除非 `i18n.prefix` 是 `always`。已经以某个语言开头的链接，指向任何区域之外的页面、公共文件或其他站点的链接，以及相对链接和锚点，都保持原样。查询和 `#fragment` 会保留。只有 Markdown 会被改写：你的 Go 代码构造的 HTML 不会。没有 `Languages` 的集合，转换方式和以前一样。

## 草稿和日期 {#drafts-and-dates}

- `draft: true` 是草稿。`tifa build` 会把它排除在外，所以它的文字不在程序里。
- `date` 还没到的条目在程序里，但 `All`、`In` 和 `Get` 不会返回它，其他条目的 `Translations` 也不会列出它。到了那个时刻，它会自己出现，不需要重新构建或重启。日期相同的条目会一起出现。
- 只写日期（`2026-11-20`）表示 UTC 午夜。要给确切的时间，请写带时区的 RFC 3339：`2026-11-20T09:00:00+08:00`。
- 程序以机器的时钟（`time.Now`）为准，不用 `tifa.WithRenderClock` 的渲染时钟。一个定时器会在下一个日期唤醒它，并且至少每分钟一次，所以当机器的时钟被设置，或机器从睡眠中醒来时，日期已过的条目会在一分钟之内出现。

`tifa build` 和 `tifa check` 会列出在等日期的条目，这样你可以在发布之前检查日期：

```text
tifa: content: 2 entries of blog wait for their date: zh-CN/202611/launch and en/202611/release-notes at 2026-11-20 09:00 +08:00
```

运行中的程序在条目出现时会记录一行：

```text
tifa: content: blog zh-CN/202611/launch and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)
```

那个时刻还会有什么变化：

| 位置 | 发生什么 |
| --- | --- |
| 带 `Cache` 的页面 | 它们的 `Load` 在每个请求上都运行，所以下一个请求会读到新条目。数据没变的页面，来自渲染记忆 memo，不需要重新渲染。 |
| 条目的页面 | 之前它响应 404，在那个时刻它就能打开了。 |
| 程序前面的 CDN | 带 `Cache` 的页面发出时，`s-maxage` 被设成到那个时刻为止的时间，所以 CDN 最多可能在这么长的时间内显示旧的列表。 |
| RSS、站点地图和其他处理函数 | 它们在每个请求上读取集合，所以有这个条目：`tifa.SetSitemap` 的函数，在每次请求 `/sitemap.xml` 和 `/llms.txt` 时也会运行。在 `OnStart` 里根据内容算好并保留下来的东西则没有。 |

读取集合不止一次的一个请求，可能会同时看到那个时刻的两边，就像刚写入数据库的一行也可能出现这种情况一样。

## `tifa dev` {#tifa-dev}

`tifa dev` 从 `.tifa/dist` 读取内容，所以改动 `content/` 下的内容不需要 go build，也不需要重启（见[每种改动会触发什么](../build-and-deploy/dev-reloads.zh-CN.md#what-each-change-triggers)）：

- Markdown 文件被修改、添加或删除，不管是正文还是 front matter：`tifa dev` 会重新扫描 `content/`，转换那个条目，写入 `.tifa/dist/content/` 和 `content.json`，运行中的应用加载它们。应用对每个条目运行 `Validate` 并取用新数据，之后你加载的页面会显示它。`OnStart` 不会再运行，打开的页面会重新加载。
- `content/` 下的其他文件，比如图片：它所在文件夹里的条目会重新转换，回退到那个文件夹的翻译也一样。
- `content.go` 或某个 schema 类型：Go 的步骤会运行，每个条目都会重新转换。
- `vite.config.ts` 或它 import 的某个插件：在 Vite 的开发服务器重启之后，每个条目都会重新转换，没有页面的应用则是立即转换。
- 不符合要求的 front matter、未声明的文件夹或插件的错误，会在页面的浮层里显示 `tifa build` 的消息。没通过 `Validate` 的条目会显示 `The app did not load the content`。不管是哪种，应用都会保留它原来的内容，包括正文，直到你修好它。
- 草稿和 `date` 还没到的条目都会显示出来，这样你在写一篇文章时，可以在列表里和它自己的页面上看到它：草稿的 `Draft` 为 true，`date` 还没到的条目，带着那个 `Date` 出现在那里。页面可以把它们标出来，就像测试应用的列表用 `{p.draft && <span>draft</span>}` 做的那样。

## 构建写出什么 {#what-the-build-writes}

`tifa build` 写出 `dist/content.json` 和 `dist/content/`，程序会把它们和 `dist/` 的其余部分一起内嵌：

| 输出 | 里面有什么 |
| --- | --- |
| `dist/content.json` | 每个集合里不是草稿的条目：`content.Entry` 的字段、翻译键、正文的文件、它的标题，以及按 schema 转成 JSON 的 front matter |
| `dist/content/<collection>/<language>/<ID>.html` | 每个条目的正文，没有语言的集合不带语言文件夹。服务器不提供这些文件，构建也不压缩它们。草稿永远不会被转换，所以它的文字不在程序里 |
| `dist/content/<collection>/<language>/<ID>.md` | 每个条目的 Markdown，放在它的正文旁边，里面的 URL 是 HTML 的 URL，应用有 `site.url` 时写成完整的 URL，并且不含注释。它是把这个条目列为自己的 `Content` 的页面的 [Markdown 版本](../guides/seo.zh-CN.md#markdown-versions)。草稿在这里也被排除 |
| `dist/assets/<name>-<hash>.<ext>` | 正文引用的文件 |

`go:embed` 会拒绝其正文文件的 ID（比如含有 `'` 的）会让构建失败，正文文件会位于另一个正文文件之下的条目（比如 `a.html/b.md` 紧挨着 `a.md`）也一样。应用有 `content.go` 时，生成的 `main` 会 import 根包，并在 `OnStart` 之前读取 `content.json`。`tifa dev` 把同样的文件写进 `.tifa/dist/`，应用从那里读取它们。

## 测试 {#tests}

`go test` 没有构建输出，所以测试用 `tifatest` 读取内容（`app/blog/post_test.go`）：

```go
func TestBySlugFindsTheTranslation(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := BySlug("en", "free-web-search")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "202608/free-search" || len(p.Translations) != 2 || p.Translations[0].Slug != "free-search" || p.Words == 0 {
		t.Errorf("post %+v", p)
	}
	if _, err := BySlug("zh-CN", "wip"); err == nil {
		t.Error("a draft was found")
	}
}

func TestTagged(t *testing.T) {
	tifatest.SetContent(t, contenttest.Blog,
		contenttest.Post{Entry: content.Entry{ID: "a", Language: "en", Slug: "a", Path: "a"}, Tags: []string{"go"}},
		contenttest.Post{Entry: content.Entry{ID: "b", Language: "en", Slug: "b", Path: "b"}},
	)
	if got := Tagged("en", "go"); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("Tagged = %+v", got)
	}
}
```

- `tifatest.LoadContent(t)` 从当前文件夹往上找到 `go.mod` 来定位应用，用和构建相同的扫描和检查来读取 `content/`，排除草稿，隐藏日期未到的条目，并运行 `Validate`。有问题时，测试会以构建给出的消息失败。
- `tifatest.SetContent(t, c, entries...)` 把给定的条目设为某个集合的内容。`content.Entry` 的字段由你来写，`Translations` 除外，它会把 ID 相同的条目连起来。日期在未来的条目会被隐藏。不运行 `Validate`。
- `tifatest.SetContentTime(t, tm)` 让集合按 `tm` 而不是现在，来决定哪些条目可以被看到：条目的日期不晚于 `tm` 时就显示。可以在 `LoadContent` 和 `SetContent` 之前或之后调用它。时间在整个测试里都保持在 `tm`：

```go
tifatest.LoadContent(t)
tifatest.SetContentTime(t, time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)) // the entries that can be seen at this time
```

- 测试没有构建，所以没有 HTML。`LoadContent` 或 `SetContent` 生成的条目的 `Body()` 返回一个写明条目的占位文本（集合、语言和 ID），`Headings()` 返回 nil，所以文章页的 `Load` 在测试里照样能运行：

```go
if got := p.Body(); got != "<p>tifatest: body of blog zh-CN/202608/free-search</p>" {
	t.Errorf("Body() = %q", got)
}
```

  要检查 HTML 本身，请测试构建出的程序。
- 这三个函数都会在测试结束时，把集合和它们的时间恢复原样。
- 集合由整个测试二进制文件共享，所以调用其中任何一个的测试都不能并行运行。当测试或它的某个上级调用了 `t.Parallel` 时，它会失败，在它之后调用 `t.Parallel` 会 panic。
- 在测试里使用集合却没有调用其中任何一个，会 panic：

```text
content: collection blog has no data: in a test call tifatest.LoadContent(t) or tifatest.SetContent
```

## 错误 {#errors}

构建把每个问题打印成 `file:line:col: reason` 的形式。

声明方面：

```text
app/blog/post.go:12:9: content.Define declares a collection, which is written only as the value of a package-level var in content.go in the root of the app, such as var Blog = content.Define[Post](content.Source{Dir: "blog"})
content.go:9:30: the argument of content.Define must be a content.Source literal written in place, so that tifa build can read it
content.go:9:44: Dir "blog/posts" must be the name of one folder under content/: letters, digits, - and _
content.go:9:44: content/blog does not exist: create it, or fix Dir
content.go:9:60: Languages has "en" twice
content.go:10:7: Docs reads content/blog, which Blog reads too (content.go:9:7)
content.go:9:22: contenttest.Post must be a struct type of this module that embeds content.Entry, not a pointer to it
content.go:21:2: contenttest.Post.Date has the json name "date", which content.Entry uses for the time an entry is published
content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/
```

文件和 front matter 方面：

```text
content/blog/fr: not a language of the collection blog (zh-CN, en), set in content.go:9:60
content/blog/notes.md: the collection blog has languages, so its files go in a language folder, such as content/blog/zh-CN/notes.md
content/blog/zh-CN/202610/demo/index.mdx: MDX is not supported: a body becomes HTML at build time and cannot hold React components. Rename it to index.md and put the components in the page
content/blog/zh-CN/a/index.md:1:1: no front matter: an entry starts with --- and the keys of contenttest.Post
content/blog/zh-CN/202608/free-search/index.md:5:8: draft: want true or false, got "yes"
content/blog/en/202608/free-search/index.md:1:1: front matter has no "title", which contenttest.Post.Title needs. Make the field a pointer or add omitempty to make it optional
content/blog/en/202608/free-search/index.md:2:7: slug: "free/search" is not one URL segment: it holds /
content/docs/en/guide/index.md: the ID guide is also the ID of content/docs/en/guide.md
```

在 `tifa build` 和 `tifa dev` 里转换 Markdown 时：

```text
the app has content collections, so tifa build turns Markdown into HTML with unified: pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
remark-gfm 5.0.0 is not a version tifa is tested with: pnpm add -D remark-gfm@4
content/blog/en/202608/free-search/index.md:43:1: image ./cover.jpg not found: looked at content/blog/en/202608/free-search/cover.jpg and content/blog/zh-CN/202608/free-search/cover.jpg
content/blog/zh-CN/a/index.md:12:5: ./b.md is a Markdown file and has no URL of its own: link to the page that shows it, such as /zh-CN/blog/b
content/blog/zh-CN/a/index.md:14:1: image ../../../notes/x.png leads to content/notes/x.png, which is outside content/blog: a collection holds only its own files
```

应用里抛出错误的插件，会在消息里被点名：`rehype plugin rehypeLazyImages`，后面是插件所说的内容。

程序启动时：

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
content: collection blog has no data yet: read it from OnStart on, not while packages are initialized
content: Get on collection docs, which has no languages, with language "en": pass ""
```
