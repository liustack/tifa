---
summary: '应用怎样把文章、文档等写作内容作为 Markdown 文件放在 content/ 下并在 Go 里读：在 content.go 里声明集合，哪些文件是条目，ID、slug、path 和译文，schema struct 允许哪些 front matter 键，草稿和到点发布的条目，正文怎样转成 HTML（步骤、remark 和 rehype 插件、图片和链接、标题）以及页面怎样显示它，tifa build 写出什么，测试里怎样读内容，以及报错和提示'
read_when:
  - 给 tifa 应用加博客、文档或其他 Markdown 内容
  - 写或改 content.go，或嵌入 content.Entry 的 schema 类型
  - 构建报 content.Define、content/ 下某个文件夹或某个文件 front matter 的错
  - 用 Body 和 <HTML> 显示条目的正文，用 Headings 做目录，或者在 vite.config.ts 的 tifa() 里加 remark、rehype 插件
  - 正文里的图片或链接找不到，或者插件让转换失败
  - 用 date 让条目在设定的时间发布，或者条目到了日期没有出现
  - 写读集合的 Go 测试，用 tifatest.LoadContent、tifatest.SetContent 或 tifatest.SetContentTime
  - 改 content 包、internal/content、internal/analyze 里的内容检查，或 js/vite/plugin.js 里转换 Markdown 的步骤
---

# 内容

集合是 `content/` 下的一个 Markdown 文件夹，应用在 Go 里把它当作有类型的值来读。构建读这些文件，按一个 Go struct 检查每个 front matter，把每篇正文转成 HTML，全部放进程序。运行时 `Load` 或 handler 像读别的数据一样读条目，不解析任何东西。

## 声明集合

集合在应用根目录的 `content.go` 里声明：

```go
// content.go
package myblog

import (
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs"})
)

type Post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}
```

| `Source` 字段 | 含义 |
| --- | --- |
| `Dir` | 必填。`content/` 下放这个集合的文件夹，也是它在报错和构建产物里的名字。只能是字母、数字、`-` 和 `_`，不能含 `/`。 |
| `Languages` | 可选。集合的语言，默认语言在前。有了它，`content/<Dir>/` 下第一层是每种语言一个文件夹。 |
| `Match` | 可选。只留下一部分文件的模式，见[哪些文件是条目](#哪些文件是条目)。 |

构建只读 `content.go`，不运行它，所以：

- `content.Define` 只能写成 `content.go` 里包级 `var` 的值。写在别处（别的文件、函数、`init()`、赋值）构建失败。
- 参数是就地写的 `content.Source{...}` 字面量，字段带名字。`Dir` 和 `Match` 是字符串字面量，`Languages` 是由字符串字面量组成的 `[]string{...}`，不能用常量或变量。
- 类型是应用本模块里的具名 struct，嵌入 `content.Entry`（不是指向它的指针）。
- 两个集合不能读同一个 `Dir`。`content/` 下没有任何集合读的文件夹会让构建失败，没有 `content.go` 的应用也一样，用来抓文件夹名的拼写错误。名字以 `.` 或 `_` 开头的文件夹不管。

根包不导入 `app/` 下的包（见[目录结构](structure.zh-CN.md)），所以 schema 类型写在 `content.go` 里，或者写在不导入任何领域的共享包里。集合的查询（`Published`、`BySlug` 这些）写在领域包里，领域包读 `myblog.Blog`，和读 `myblog.DB` 一样。

## 哪些文件是条目

```text
content/
  blog/
    zh-CN/
      202608/free-search/index.md     一个条目（页面包）
      202608/free-search/cover.jpg    属于它
      202608/free-search/wechat.md    属于它，不是条目
      about.md                        一个条目（单个文件）
    en/
      202608/free-search/index.md
  docs/
    index.md  install.md  guide.md    各是一个条目
    guide/routing.md                  单独一个条目
```

按顺序：

1. 名字以 `.` 或 `_` 开头的文件和文件夹跳过。
2. 有 `index.md` 的文件夹是一个条目，叫页面包。它下面的所有东西，包括别的 `.md` 和子文件夹，都属于这个条目。集合文件夹和语言文件夹不算页面包：放在那里的 `index.md` 是单个文件，ID 是 `index`。
3. 其余每个 `.md` 文件各是一个条目。
4. 写了 `Match` 时，上面两条选出的文件里只有匹配它的才是条目。模式从语言文件夹下面算起（没有语言就从集合文件夹下面算起），用 `/` 分段，每段的写法同 `path.Match`，`**` 匹配任意多层文件夹。`Match: "*/*/index.md"` 只要两层深的页面包，不要单个文件。
5. `.mdx` 文件让构建失败。其他扩展名的文件不是条目。

符号链接可以指向集合文件夹里的文件或文件夹。指到集合文件夹外面，或者指向包含这个链接的文件夹，构建失败，这样别处的文件不会进到程序里。

一组文档的首页写成和文件夹同名的文件（`guide/` 旁边放 `guide.md`）。写成 `guide/index.md` 会让整个 `guide/` 成为一个页面包，两者都有时构建报 ID 重复。

## ID、slug、path 和语言

- **ID**：文件在集合文件夹和语言文件夹下面的路径，去掉扩展名和结尾的 `/index`。`content/blog/zh-CN/202608/free-search/index.md` 的 ID 是 `202608/free-search`。front matter 改不了它。集合的同一种语言里每个 ID 只有一个。
- **Slug**：front matter 的 `slug`，没写就是 ID 的最后一段。它必须是一个 URL 段：不为空，不是 `.` 或 `..`，不含 `/`、`?`、`#`、`%`、空白和控制字符。中文等文字可以用。
- **Path**：ID 的最后一段换成 slug，没写 `slug` 时就是 ID。集合的同一种语言里每个 path 只有一个。slug 可以重复（`guide/install` 和 `api/install` 都是 `install`）。用 slug 拼 URL 的博客在自己的测试里检查 slug 不重复。

有 `Languages` 时，所有条目都在语言文件夹里，默认语言也一样。第一层出现不是语言的文件夹，或者 `.md` 文件直接放在集合文件夹里，构建失败。某种语言没有文件夹，就是没有条目。没有 `Languages` 就没有语言这一层，`Entry.Lang` 是 `""`。

同一个条目的各语言版本共用一个译文键：front matter 的 `translationKey`，没写就是 ID。所以路径相同的文件自动互为译文，路径不同的用 `translationKey` 连起来。一种语言里每个键只有一个。`Entry.Translations` 按 `Languages` 的顺序列出现在能看到的各个版本，包括它自己，每项是 `{Lang, ID, Slug, Path}`。语言切换链接和 `hreflang` 从这里取。

某种语言缺一个版本没关系。构建每个集合、每种缺的语言打一行：

```text
tifa: content: 2 entries of docs have no zh-CN version: guide, guide/routing (warning)
```

## Front matter

每个条目以 YAML 写的 front matter 开头，前后各一行 `---`。它的键是 schema struct 字段的 `json` 名，构建按 `Load` 返回值和 handler 输入同一套 JSON 规则检查（见 [JSON 规则](app.zh-CN.md#json-规则)）。

- 字段可以是字符串、数字、bool、`time.Time`、指针、切片、字符串键的 map 或嵌套 struct。`time.Time` 接受 `2006-01-02`（按 UTC 零点算）和 RFC 3339，带不带引号都行。自己读 JSON 的类型、`[]byte`、`time.Duration`、接口，以及 `omitempty`、`omitzero` 之外的 json 选项（比如 `,string`）让构建失败。
- 不是指针、切片、map，也没有 `omitempty` 或 `omitzero` 的字段是必填，和页面 TypeScript 类型里的可选字段一致。
- 不是字段的键让构建失败，有接近的字段名时报错里会给出。
- 字段写了 `yaml` tag 构建失败：键就是 json 名。
- 有四个键由 tifa 自己读。它们不是 schema 的字段，写了也不算错：

| 键 | 含义 |
| --- | --- |
| `date` | 条目发布的时间，即 `Entry.Date`。只写日期（`2026-11-20`）按 UTC 零点算。要精确到时刻，写带时区的 RFC 3339：`2026-11-20T09:00:00+08:00`。 |
| `draft` | `true` 表示草稿。 |
| `slug` | slug，见上文。 |
| `translationKey` | 把一个条目的各语言版本连起来。 |

- `id`、`lang`、`path`、`translations` 和 `words` 由 tifa 设置，写了构建失败。schema 字段用了 `content.Entry` 的某个 json 名，或者叫 `translationKey`，构建也失败。

`content.Entry` 放的是 tifa 设置的东西：

```go
type Entry struct {
	ID           string        `json:"id"`
	Lang         string        `json:"lang"`
	Slug         string        `json:"slug"`
	Path         string        `json:"path"`
	Date         time.Time     `json:"date,omitzero"`
	Draft        bool          `json:"draft,omitzero"`
	Translations []Translation `json:"translations"`
	Words        int           `json:"words"`
}
```

`Words` 是正文的字数：每个中文、日文或韩文字符算一个，其他文字里连续的字母和数字算一个。代码块、HTML 注释和标签、链接地址不算。

### 构建查不了的规则：Validate

构建不运行应用的代码。允许的取值、URL 前缀这类规则写在 schema 的 `Validate() error` 方法里：

```go
func (p Post) Validate() error {
	if p.Cover != "" && !strings.HasPrefix(p.Cover, "https://static.example.com/") {
		return errors.New("cover must start with https://static.example.com/")
	}
	return nil
}
```

程序启动时，在 `OnStart` 之前对每个条目跑一遍。有一个失败，程序打出它并退出，不监听：

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
```

`tifatest.LoadContent` 也会跑它，所以 `go test` 和 `tifa check` 的测试步骤在部署前就能发现。

## 草稿和日期

- `draft: true` 是草稿。`tifa build` 不收它，所以它的文字不在程序里。
- `date` 还没到的条目在程序里，但 `All`、`In`、`Get` 不返回它，别的条目的 `Translations` 也不列它。到了那一刻它自己出现，不用重新构建，也不用重启。日期相同的条目一起出现。
- 只写日期（`2026-11-20`）按 UTC 零点算。要精确到点，写带时区的 RFC 3339：`2026-11-20T09:00:00+08:00`。
- 程序按机器的时钟（`time.Now`）判断，和 `tifa.WithRenderClock` 的渲染时钟无关。

`tifa build` 和 `tifa check` 列出等待发布的条目，上线前可以核对时间写得对不对：

```text
tifa: content: 2 entries of blog wait for their date: zh-CN/202611/launch and en/202611/release-notes at 2026-11-20 09:00 +08:00
```

运行中的程序在条目发布时打一行日志：

```text
tifa: content: blog zh-CN/202611/launch and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)
```

那一刻其他地方的变化：

| 地方 | 会怎样 |
| --- | --- |
| 带 `Cache` 的页面 | 整页缓存里在这一刻之前做好的页面全部算作过期，下一个请求重新生成。数据没变的页面由渲染缓存回答，不重新渲染。 |
| 条目自己的页面 | 之前回答 404，而 404 不进整页缓存，所以到点就能打开。 |
| 程序前面的 CDN | 带 `Cache` 的页面发出时 `s-maxage` 就是这个时长，CDN 可能在这段时间内还给旧的列表。 |
| RSS、sitemap 等 handler | 每次请求都读集合，所以有这个条目。在 `OnStart` 里根据内容算好存起来的东西没有，所以要在用到它的函数里现算。 |

一次请求里多次读集合，可能正好跨过这一刻，和数据库里刚写入一行时一样。

## 读集合

| `*content.Collection[T]` 的方法 | 返回 |
| --- | --- |
| `All() []T` | 所有语言现在能看到的条目，按 ID 排序，同一个 ID 按 `Languages` 的顺序。每次返回一个新切片，调用方可以随意排序或修改。 |
| `In(lang string) []T` | 一种语言的条目，按 ID 排序。集合没有这种语言时返回 `nil`。 |
| `Get(lang, id string) (T, error)` | 一个条目。没有或还看不到时返回 `tifa.ErrNotFound`，`Load` 可以原样返回这个错误。没有语言的集合 `lang` 传 `""`，传别的会 panic。 |
| `Languages() []string` | 声明的语言，默认语言在前。 |

按日期或 `order` 字段排序、按 slug 或标签查找，都是领域里几行 Go：

```go
func Published(lang string) []myblog.Post {
	posts := myblog.Blog.In(lang)
	slices.SortFunc(posts, func(a, b myblog.Post) int { return b.Date.Compare(a.Date) })
	return posts
}
```

内容在 `OnStart` 之前载入，所以 `OnStart` 里能读。包初始化期间读集合会 panic。到了某个条目的日期，能看到的条目就会变，所以依赖内容算出来的东西在用到它的函数里现算，比如 RSS 的 handler，不要在 `OnStart` 里存一份。

## 显示正文

`Body()` 和 `Headings()` 是 `content.Entry` 的方法，不是字段，所以页面数据里放一列条目时不会带上正文。文章页自己把它们放进数据：

```go
type Page struct {
	Post     myblog.Post       `json:"post"`
	Body     tifa.HTML         `json:"body"`
	Headings []content.Heading `json:"headings"`
}

func Load(c *tifa.Ctx) (Page, error) {
	// ...
	return Page{Post: p, Body: p.Body(), Headings: p.Headings()}, nil
}
```

```tsx
import { HTML } from 'tifa/client'

export default function PostPage() {
  const { post, body, headings } = page.useLoaderData()
  return (
    <article>
      <h1>{post.title}</h1>
      <nav>
        {(headings ?? []).filter((h) => h.depth === 2).map((h) => (
          <a key={h.id} href={`#${h.id}`}>{h.text}</a>
        ))}
      </nav>
      <HTML value={body} className="prose" />
    </article>
  )
}
```

- `Body() tifa.HTML` 每次调用都从程序里读这篇的 HTML（`tifa dev` 从 `.tifa/dist` 读），不留副本。页面用 `<HTML>` 显示它，见[显示应用信任的 HTML](app.zh-CN.md#显示应用信任的-html)。
- `Headings() []content.Heading` 按顺序列出正文里的 `h1` 到 `h6`，每项是 `{Depth, ID, Text}`：级别、它在 HTML 里的 `id` 和文字。
- `Words` 数的是 Markdown，不用转换。阅读时长由应用自己算，比如 `Math.max(1, Math.ceil(words / 300))` 分钟。

## Markdown 转 HTML

`tifa build` 在 `vite.config.ts` 的 tifa 插件里用 [unified](https://unifiedjs.com/) 把每篇的 Markdown 转成 HTML。有集合的应用要装这几个包，主版本如下：

```sh
pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
```

缺了某个包，或者主版本不对，构建失败并给出安装命令。只有声明了集合的应用，tifa 才载入它们。转换在 Vite 里进行，所以集合里有条目的应用即使没有页面，也要有 `package.json` 和用了 tifa 插件的 `vite.config.ts`，比如只有订阅源或接口、在里面写出 `Body()` 的应用。没有页面时，构建用 `vite.config.ts` 里的插件转换，不打包任何东西，正文引用的文件放进 `dist/assets/`，文件名带哈希。`tifa check` 检查 front matter 不需要 Node。

每篇按这个顺序处理：

1. front matter 换成同样行数的空行，所以报错里的行号就是文件里的行号。
2. `remark-parse` 和 `remark-gfm`（表格、删除线、任务列表、自动链接）。
3. 应用的 `remarkPlugins`。
4. `remark-rehype` 和 `rehype-raw`：Markdown 里写的 HTML 原样保留。内容是仓库的一部分，是可信的，不做过滤。
5. 去掉 HTML 注释，写给作者的笔记不会出现在页面上。
6. 没有 `id` 的 `h1` 到 `h6` 按 GitHub 的规则生成 `id`，和 `rehype-slug` 一样：文字转小写，去掉标点，空格换成 `-`，重复的加 `-1`、`-2` 等。HTML 里写了 `id` 的不动。
7. 应用的 `rehypePlugins`。它们在 id 之后运行，所以 `rehype-autolink-headings` 这类插件能直接用。
8. 改写相对 URL，见下文。
9. 收集标题给 `Headings()`，用的是最终的 id。
10. `rehype-stringify`。

应用的插件写在 `vite.config.ts` 的 `tifa()` 里，写法和 unified 一样，一个插件或者 `[插件, 选项]`：

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import rehypeShiki from '@shikijs/rehype'
import pangu from 'remark-pangu'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [
    tifa({
      markdown: {
        remarkPlugins: [pangu],
        rehypePlugins: [[rehypeShiki, { theme: 'github-light' }]],
      },
    }),
    react(),
  ],
})
```

- tifa 的步骤不能关。不想要某个效果（比如 GFM 把裸网址变成链接），用自己的插件改回来。
- 每篇的 vfile 带 `file.path`（Markdown 文件的完整路径）和 `file.data.tifa`，后者是 `{ collection, lang, id, frontmatter }`，front matter 是按 schema 转好的 JSON。要读条目旁边某个文件的插件，从 `file.path` 找。
- 插件抛错时构建失败，报错带文件、插件给的位置和插件的名字。插件加的警告会打出来。
- 不支持 MDX：正文是 HTML，放不了 React 组件。组件写在页面里。
- `tifa build` 每次都转换全部条目。`tifa dev` 每次构建包时都转换全部条目。

### 图片和链接

正文里的相对 URL（不以 `/`、`#` 或 `https:` 这类协议开头）在这些地方改写：`img`、`video`、`audio`、`source` 的 `src`，`video` 的 `poster`，`a` 的 `href`。

- 先在 Markdown 文件旁边找。没有的话，到这篇在默认语言的版本旁边找。所以图片放一份就够，放在默认语言旁边。译文要换图，就在译文旁边放一张同名的。
- 找到的文件随浏览器包输出为 `/assets/<名字>-<哈希>.<扩展名>`，和其他资源一样长期缓存。几篇用同一个文件时只有一份。`tifa dev` 下 URL 指向 Vite 开发服务器上的这个文件。
- 文件必须在集合的文件夹里：带 `..` 的 URL 或符号链接指到外面时，构建失败。
- 查询串和 `#片段` 留在新 URL 上。
- 指向 `.md` 文件的相对链接让构建失败。Markdown 文件没有自己的 URL，页面的 URL 由应用的路由决定，所以链接写页面的地址，比如 `/zh-CN/blog/b`。
- front matter 里的 URL 是普通字符串，不改写。本地图片不加 `width`、`height`、`srcset`，需要时用自己的 rehype 插件加。

## 构建写出什么

`tifa build` 写出 `dist/content.json` 和 `dist/content/`，程序把它们和 `dist/` 里别的文件一起嵌入：

| 产物 | 内容 |
| --- | --- |
| `dist/content.json` | 每个集合中不是草稿的条目：`content.Entry` 的字段、译文键、正文文件、它的标题，以及按 schema 转成 JSON 的 front matter |
| `dist/content/<集合>/<语言>/<ID>.html` | 每篇的正文，没有语言的集合去掉语言这层。服务端不经 HTTP 提供这些文件，构建也不压缩它们。草稿从不转换，所以它的文字不在程序里 |
| `dist/assets/<名字>-<哈希>.<扩展名>` | 正文引用的文件 |

正文文件名 go:embed 不收的 ID（比如含 `'`）让构建失败。一篇的正文文件落在另一篇正文文件下面时也失败，比如 `a.md` 旁边的 `a.html/b.md`。应用有 `content.go` 时，生成的 `main` 导入根包，并在 `OnStart` 之前读 `content.json`。`tifa dev` 把同样的文件写到 `.tifa/dist/`，应用从那里读。

## 测试

`go test` 拿不到构建产物，所以测试用 `tifatest` 读内容：

```go
func TestBySlugFindsTheTranslation(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := blog.BySlug("en", "free-web-search")
	// ...
}

func TestTagged(t *testing.T) {
	tifatest.SetContent(t, myblog.Blog,
		myblog.Post{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a"}, Tags: []string{"go"}},
	)
	// ...
}

func TestScheduled(t *testing.T) {
	tifatest.LoadContent(t)
	tifatest.SetContentTime(t, time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)) // 集合按这个时刻判断哪些条目能看到
	// ...
}
```

- `tifatest.LoadContent(t)` 从当前文件夹往上找到 `go.mod`，把那里当作应用，用和构建同一套扫描和检查读 `content/`，去掉草稿，隐藏日期还没到的条目，再跑 `Validate`。有问题时测试失败，消息和构建给的一样。
- `tifatest.SetContent(t, c, entries...)` 让一个集合的内容换成给定的条目。`content.Entry` 的字段就是你写的，只有 `Translations` 按相同 ID 连起来。日期在未来的条目会隐藏。不跑 `Validate`。
- `tifatest.SetContentTime(t, tm)` 让集合按 `tm` 而不是现在判断哪些条目能看到：日期不晚于 `tm` 的条目显示。在 `LoadContent` 和 `SetContent` 之前或之后调用都行。整个测试里时间停在 `tm`。
- 测试没有构建，所以没有 HTML：`LoadContent` 和 `SetContent` 给的条目，`Body()` 返回占位 `<p>tifatest: body of blog zh-CN/202608/free-search</p>`（集合、语言和 ID），`Headings()` 返回 nil。文章页的 `Load` 照样能在测试里跑。要检查 HTML 本身，测构建好的程序。
- 三者都在测试结束时把集合和它们的时间恢复原样。
- 集合由整个测试程序共用，所以调用其中任何一个的测试都不能并行。测试本身或它的上层调用过 `t.Parallel` 时，测试失败。调用之后再调 `t.Parallel` 会 panic。
- 测试里不调用其中一个就用集合会 panic：

```text
content: collection blog has no data: in a test call tifatest.LoadContent(t) or tifatest.SetContent
```

## 报错

构建把每个问题打成 `file:line:col: reason`。

声明：

```text
app/blog/post.go:12:9: content.Define declares a collection, which is written only as the value of a package-level var in content.go in the root of the app, such as var Blog = content.Define[Post](content.Source{Dir: "blog"})
content.go:9:30: the argument of content.Define must be a content.Source literal written in place, so that tifa build can read it
content.go:9:44: Dir "blog/posts" must be the name of one folder under content/: letters, digits, - and _
content.go:9:44: content/blog does not exist: create it, or fix Dir
content.go:9:60: Languages has "en" twice
content.go:10:7: Docs reads content/blog, which Blog reads too (content.go:9:7)
content.go:9:22: myblog.Post must be a struct type of this module that embeds content.Entry, not a pointer to it
content.go:21:2: myblog.Post.Date has the json name "date", which content.Entry uses for the time an entry is published
content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/
```

文件和 front matter：

```text
content/blog/fr: not a language of the collection blog (zh-CN, en), set in content.go:9:60
content/blog/notes.md: the collection blog has languages, so its files go in a language folder, such as content/blog/zh-CN/notes.md
content/blog/zh-CN/202610/demo/index.mdx: MDX is not supported: a body becomes HTML at build time and cannot hold React components. Rename it to index.md and put the components in the page
content/blog/zh-CN/a/index.md:1:1: no front matter: an entry starts with --- and the keys of myblog.Post
content/blog/zh-CN/202608/free-search/index.md:5:8: draft: want true or false, got "yes"
content/blog/zh-CN/202608/free-search/index.md:9:1: "tag" is not a field of myblog.Post. Did you mean "tags"? Fields: title, description, updated, tags, cover
content/blog/en/202608/free-search/index.md:1:1: front matter has no "title", which myblog.Post.Title needs. Make the field a pointer or add omitempty to make it optional
content/blog/en/202608/free-search/index.md:2:7: slug: "free/search" is not one URL segment: it holds /
content/docs/en/guide/index.md: the ID guide is also the ID of content/docs/en/guide.md
```

转换 Markdown 时（`tifa build` 和 `tifa dev`）：

```text
the app has content collections, so tifa build turns Markdown into HTML with unified: pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
remark-gfm 5.0.0 is not a version tifa is tested with: pnpm add -D remark-gfm@4
content/blog/en/202608/free-search/index.md:43:1: image ./cover.jpg not found: looked at content/blog/en/202608/free-search/cover.jpg and content/blog/zh-CN/202608/free-search/cover.jpg
content/blog/zh-CN/a/index.md:12:5: ./b.md is a Markdown file and has no URL of its own: link to the page that shows it, such as /zh-CN/blog/b
content/blog/zh-CN/a/index.md:14:1: image ../../../notes/x.png leads to content/notes/x.png, which is outside content/blog: a collection holds only its own files
content/blog/zh-CN/a/index.md:18:1: rehype plugin rehypeContentImages: https://static.example.com/assets/0f/0f9c.jpg is not in the assets.yaml of this entry
```

程序启动时：

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
content: collection blog has no data yet: read it from OnStart on, not while packages are initialized
content: Get on collection docs, which has no languages, with lang "en": pass ""
```
