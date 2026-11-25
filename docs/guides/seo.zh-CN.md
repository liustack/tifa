---
summary: '搜索引擎和语言模型怎样找到 Tifa 站点：site.url、sitemap.xml、robots.txt、llms.txt、og 标签'
audience: app
order: 8
translatedFrom: 4ac7dae270a3
read_when:
  - '设置页面的标题、描述或分享卡片（`og:title`、`og:image`、`twitter:card`），或者站点链接在聊天或信息流里不显示图片'
  - '让搜索引擎收录 Tifa 站点，或者添加 sitemap 或 robots.txt'
  - '让 ChatGPT、Claude 和其他语言模型通过 llms.txt 或页面的 Markdown 读取站点'
  - '日志说 sitemap.xml 只列出了不带参数的页面'
  - '/sitemap.xml 或 /llms.txt 返回 500，某个页面不在其中或者不该在其中，或者日志里有来自 SetSitemap 的一行'
  - '让需要登录、购物车或结账的页面不进入 sitemap'
---

# 搜索引擎和语言模型 {#search-engines-and-language-models}

你希望别人能在搜索结果里，以及在 ChatGPT、Claude 这类工具的回答里找到你的站点。做完之后，搜索引擎会拿到一份 sitemap，里面有每个公开页面、它的日期和它其他语言的版本，语言模型会拿到一份简短的站点索引，以及你的文章的 Markdown 文本。

这一切来自一个设置和一个函数。下面的代码来自 [test/apps/i18n](../../test/apps/i18n) 和 [examples/shop](../../examples/shop/sitemap.go)。

## 打开它 {#turn-it-on}

在 `tifa.yaml` 里设置站点的地址：

```yaml
# tifa.yaml
site:
  url: https://shop.example.com
```

`site.url` 是站点所在的协议和主机，不带路径。它表示这个站点是公开的。有了它，程序会响应：

| 路径 | 它的内容 |
| --- | --- |
| `/sitemap.xml` | 每个不带参数的页面，每种语言一份，带指向它其他版本的 `hreflang` 链接 |
| `/robots.txt` | `User-agent: *`、`Allow: /` 和一行 `Sitemap:`，除非应用有 `public/robots.txt` |
| `/llms.txt` | 站点的名字和每个页面一个链接，格式见 [llmstxt.org](https://llmstxt.org) |
| `/llms-full.txt` | 每个显示内容条目的页面的 Markdown，一个接一个 |
| `<path>.md` | 显示内容条目的页面的 Markdown 版本，比如 `/blog/hello.md` |

每个页面还会得到带完整 URL 的 `<link rel="canonical">`：路径加上它的 `Load` 和布局读取的查询参数，也就是服务器渲染它时用的 URL（见 [查询参数](../reference/memo.zh-CN.md#query-parameters)）。其他参数会略去。所以当 `/store/products?page=2&utm_source=news` 的 `Load` 读取 `page` 时，它的 canonical URL 是 `/store/products?page=2`：列表的第 2 页是一个独立的页面，而带跟踪参数的链接不是副本。你在自己的 `head` 里写的 canonical 优先。404 页面和错误页面没有 canonical。

带 canonical URL 的页面还会得到 `<meta property="og:url">`，URL 相同，这样被分享的链接指向的是去掉了跟踪参数的页面。路由的 `head` 或根布局的 `documentHead` 里的 `og:url` 优先，页面上只留那一个。页面自己写了 canonical 时，`og:url` 重复那个 URL。

没有 `site.url`，这一切都不会发生：没有 canonical 链接，没有 sitemap，也没有 `robots.txt`。`public/` 里的文件，或应用在这些路径上的路由，优先于 Tifa 的，所以已经有自己 sitemap 的应用会保留它。

所有页面路径都固定的站点，比如 `/`、`/about` 和 `/pricing`，不需要再做别的。它的 sitemap 是完整的。

## 分享卡片 {#share-cards}

把你的页面链接发到聊天或社交动态里，会显示一张卡片：标题、一行文字和一张图。卡片读取 `og:` meta 标签，缺了的会被猜一个，或者留空。把它们写在页面的 `head` 里（见[标题和 meta 标签](../pages-and-data/head.zh-CN.md#title-and-meta-tags)），数据来自它的 `Load`。`og:url` 由 Tifa 自己写。

```go
// app/products/product+/route.go
type Page struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Image   string `json:"image"`
}

func Load(c *tifa.Ctx) (Page, error) {
	p, err := catalog.Get(c.Param("product"))
	if err != nil {
		return Page{}, err
	}
	return Page{Name: p.Name, Summary: p.Summary, Image: "/og/" + p.ID + ".png"}, nil
}
```

```tsx
// app/products/product+/page.tsx
export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData
    ? [
        { title: loaderData.name },
        { property: 'og:title', content: loaderData.name },
        { property: 'og:description', content: loaderData.summary },
        { property: 'og:image', content: loaderData.image },
        { name: 'twitter:card', content: 'summary_large_image' },
      ]
    : [],
})
```

- `og:image` 必须是带协议和主机的完整 URL，因为爬虫不会按页面地址去解析路径。写一个以单个 `/` 开头的路径，比如 `/og/kettle.png`，Tifa 会在前面加上 `site.url`。`og:image:url`、`og:image:secure_url` 和 `twitter:image` 也一样，在服务器上和在浏览器里导航之后都是如此。完整 URL，或以 `//` 开头的 URL，保持你写的样子。没有 `site.url` 时什么都不改，所以路径还是路径。
- 固定的图放进 `public/`，比如 `public/og/default.png`，它在 `/og/default.png` 上提供，名字不变。每个页面不同的图，可以用应用里一个写出它的 GET 路由。不要用 import 导入它，因为导入的文件会得到带哈希的名字，每次构建都会变，而爬虫会留着旧卡片。
- 图做成 1200 乘 630 像素，比例约 1.91 比 1，这样能铺满大多数应用的大卡片。PNG 或 JPEG，小于 5 MB。
- `twitter:card` 设为 `summary_large_image` 是请求大卡片。不设的话，X 显示小卡片。标题和描述取自 `og:` 标签。
- 路由里的标签，会替换上层布局里同一个 `property` 或 `name` 的标签。一个路由写了同名的多个标签，比如两个 `og:image`，就全部保留，并丢掉外层的。所以把默认卡片放在根布局的 `documentHead` 里，让页面去覆盖它。

## 列出带参数的页面 {#list-the-pages-with-parameters}

Tifa 猜不出参数的值：它不知道 `/blog/{slug}` 有哪些文章。没有帮助时，sitemap 里只有不带参数的页面，程序启动时会记一行日志，指出它略去的路由：

```text
INFO tifa: sitemap.xml lists the pages without parameters only. List the pages of /blog/{slug} with tifa.SetSitemap in OnStart, or exclude them there
```

在 `OnStart` 里用 `tifa.SetSitemap` 列出它们。它接收一个返回页面的函数：

```go
// hooks.go
func OnStart(ctx context.Context) error {
	tifa.SetSitemap(sitemap)
	return nil
}

// sitemap.go
func sitemap(c *tifa.Ctx) (tifa.Sitemap, error) {
	sm := tifa.Sitemap{
		Name:    "Acme Blog",
		Summary: "Notes on building small web apps in Go.",
		Exclude: []string{"/account", "/login"},
	}
	for _, p := range Blog.All() {
		var versions []tifa.Alternate
		for _, t := range p.Translations {
			versions = append(versions, tifa.Alternate{Language: t.Language, Path: "/blog/" + t.Slug})
		}
		sm.Pages = append(sm.Pages, tifa.SitemapPage{
			Language:    p.Language,
			Path:        "/blog/" + p.Slug,
			Modified:    p.Date,
			Title:       p.Title,
			Description: p.Description,
			Content:     p,
			Alternates:  versions,
		})
	}
	return sm, nil
}
```

这个函数在每个对 `/sitemap.xml`、`/llms.txt`、`/llms-full.txt` 和 `.md` 路径的请求上运行，所以到了日期才发布的文章，从那一刻起就在 sitemap 里了。要保持它很快：读内存里已有的东西，比如内容集合，或者只跑一次查询。`SetSitemap` 和 `SetHTTPHandler` 一样，服务器开始监听之后调用会 panic。

这个函数放在根包里，在 `hooks.go` 旁边。它可以直接读 `content.go` 里的集合。要读 `app/` 下的某个域，根包必须能导入它：见 [导入的方向](../start/structure.zh-CN.md#which-way-imports-go)。

### `tifa.Sitemap` {#tifasitemap}

| 字段 | 含义 |
| --- | --- |
| `Name` | 站点的名字，`llms.txt` 的标题。为空时是 `site.url` 的主机。 |
| `Summary` | 一两句关于站点的话，放在 `llms.txt` 的标题下面。 |
| `Exclude` | 要略去的路由模式，写法和 `c.Pattern()` 一样，比如 `/projects/{project}` 或 `/login`。每一项会略去它的页面和它下面的每个页面，所以 `/projects` 也会略去 `/projects/{project}/orders`。 |
| `Pages` | 带参数的路由的页面，以及需要标题、日期或 Markdown 版本的不带参数的页面。 |

### `tifa.SitemapPage` {#tifasitemappage}

| 字段 | 含义 |
| --- | --- |
| `Language` | 页面的语言：它所在文件夹的语言之一，在没有语言的站点上，以及对不在任何带 `_locales` 的文件夹里的页面，为空。 |
| `Path` | 访客看到的路径，已转义，不带语言前缀，也不带查询：`/blog/hello-world`。和 `tifa.Alternate` 的 `Path` 一样。 |
| `Modified` | 页面最后一次改动的时间，即 sitemap 的 `<lastmod>`，以 UTC 写出。为零值时略去。用你信得过的日期，比如文章的日期。每个请求都变的日期，会教搜索引擎忽略它。 |
| `Title`、`Description` | `llms.txt` 里这个链接的文字。没有标题时，链接显示路径。 |
| `Content` | 页面显示的内容条目，比如从 `Blog.Get` 读到的一篇文章：任何内嵌了 `content.Entry` 的类型的值，保持集合返回它时的样子。有了它，页面就有 Markdown 版本，它的文本也在 `llms-full.txt` 里。 |
| `Alternates` | 页面每种语言的版本，包括它自己，和 `c.SetAlternates` 接收的一样。为空时，页面以同一个路径出现在它文件夹的每种语言里。 |

同时也在 `Pages` 里的不带参数的页面，以 `Pages` 说的为准，所以你可以给 `/` 一个标题，或给 `/about` 一个日期。

## 略去页面 {#leave-pages-out}

需要登录的页面、购物车、结账，以及只做重定向的页面，是给使用它们的人用的，不是给搜索用的。把它们的路由模式放进 `Exclude`。这些路由反正在浏览器的包里，所以 sitemap 列出它们不会泄露什么新东西，但顺着链接来的搜索引擎会得到一个登录页面。

shop 略去了它的工作台、登录页面、购物车和结账：

```go
Exclude: []string{"/projects", "/login", "/store/cart", "/store/checkout"},
```

`Exclude` 只是让页面不出现在 sitemap 和 `llms` 文件里。页面仍然会响应，找到它链接的爬虫仍然会读它。要让爬虫不来，就把它加进 `public/robots.txt`，或在页面的 head 里发出 `noindex`。

## 多种语言 {#several-languages}

在有多种语言的站点上，每个页面每种语言列一次，每个版本带一条 `<xhtml:link rel="alternate" hreflang="...">`，默认语言还有 `x-default`，和页面 head 里的链接一样（见 [搜索引擎得到什么](i18n.zh-CN.md#what-search-engines-get)）。`Path` 不带语言前缀，并设置 `Language`：Tifa 会加上前缀。

当页面在另一种语言里有另一个 slug，或只存在于一种语言里时，在 `Alternates` 里给出它的各个版本，就像它的 `Load` 把它们交给 `c.SetAlternates` 那样。这样 sitemap 和页面的 head 就一致了。下面取自 test/apps/i18n，一篇 slug 在中文里是 `ni-hao`、在英文里是 `hello` 的文章：

```go
hello := []tifa.Alternate{{Language: "zh-CN", Path: "/store/journal/ni-hao"}, {Language: "en-US", Path: "/store/journal/hello"}}
for _, version := range hello {
	e, err := Journal.Get(version.Language, "hello")
	if err != nil {
		return tifa.Sitemap{}, err
	}
	sm.Pages = append(sm.Pages, tifa.SitemapPage{
		Language: version.Language, Path: version.Path, Modified: e.Date, Title: e.Title,
		Content: e, Alternates: hello,
	})
}
```

每个版本在 `Pages` 里都是一个独立的条目。`llms.txt` 每种语言有一个小节，默认语言在最前。

## 这些文件长什么样 {#what-the-files-look-like}

shop 的 `/sitemap.xml`，一篇文章：

```xml
<url><loc>https://shop.example.com/store/journal/kettle-photos</loc><lastmod>2026-09-03T02:00:00Z</lastmod><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/store/journal/kettle-photos"/><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/zh-CN/store/journal/kettle-photos"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store/journal/kettle-photos"/></url>
```

一个 sitemap 最多放 50,000 个 URL 和 50 MB。超过之后，`/sitemap.xml` 变成 sitemap 索引，URL 在 `/sitemap-1.xml`、`/sitemap-2.xml` 等文件里。

`/llms.txt`：

```markdown
# Shanhai Studio

> Everyday things, made to wear in: kitchenware, stationery and home goods from a small studio, with notes from the workshop.

## en-US

- [Shanhai Studio](https://shop.example.com/store): Everyday things, made to wear in
- [Journal](https://shop.example.com/store/journal): Days in the workshop, the stories behind our things, and API notes for shop owners.
- [All products](https://shop.example.com/store/products)
- [Morning Pour-Over Kettle](https://shop.example.com/store/products/pour-over-kettle): A slim spout and a steady pour for the first coffee of the day.
...
- [How we photograph a kettle](https://shop.example.com/store/journal/kettle-photos): One lamp, one white card and a lot of patience: the light, the kit and the edit behind our product photos.

## zh-CN

- [山海工作室](https://shop.example.com/zh-CN/store): 日常器物，慢慢用旧
...
```

不带参数的页面在前，按路径排序，然后是其他页面，按 `Pages` 里的顺序。带 `Content` 的页面链接到它的 Markdown 版本，也就是以 `.md` 结尾的 URL，语言模型读它比读 HTML 更容易。没有语言的站点只有一个小节，`## Pages`。

## Markdown 版本 {#markdown-versions}

带 `Content` 的页面，在它的 URL 加上 `.md` 就有一个 Markdown 版本：`/blog/hello` 对应 `/blog/hello.md`，另一种语言是 `/en-US/blog/hello.md`，`/` 对应 `/index.md`。它以标题开头，然后是条目的 Markdown：

```markdown
# Hello

[news](https://i18n.example.com/en-US/store/news) [projects](https://i18n.example.com/projects) [icon](https://i18n.example.com/favicon.svg)
```

- `tifa build` 把每个条目的 Markdown 保存在它的 HTML 旁边，草稿除外，和保存 HTML 的方式一样。链接和图片指向 HTML 里它们指向的地方，用 `site.url` 写成完整的形式：图片指向 `/assets/` 下它们的文件，以根路径开头的链接会得到条目的语言前缀（见 [图片和链接](../pages-and-data/content.zh-CN.md#images-and-links)）。在 `tifa dev` 下它们保持本地路径。
- HTML 注释会略去。front matter 不在里面：读者需要的内容放进标题。
- 响应带 `Content-Type: text/markdown; charset=utf-8`，以及一个带页面 canonical URL 的 `Link` 头，这样搜索引擎不会把它当成副本。
- 只有被列出、带 `Content` 的页面的 URL 才会响应。其他以 `.md` 结尾的路径照常交给路由，没有 `Content` 的页面没有 Markdown 版本。

`/llms-full.txt` 把每个 Markdown 版本放进一个文件，每个都带它的标题和一行 `Source:`，写着页面的 URL。

Markdown 版本和 `/llms-full.txt` 都不运行页面的 `Load`。写在 `Load` 里的检查管不到它们，所以要把检查写在 `OnRequest` 里，或者别把这个条目放进 `Content`。

## `robots.txt` {#robotstxt}

没有 `public/robots.txt` 时，程序响应：

```text
User-agent: *
Allow: /

Sitemap: https://shop.example.com/sitemap.xml
```

有了它，你的文件会原样提供，Tifa 什么都不加。请自己把 `Sitemap:` 那一行加进去。

## 请求、`OnRequest` 和错误 {#requests-onrequest-and-errors}

sitemap、`llms` 文件和 Markdown 版本和其他请求一样：`OnRequest` 会为它们运行，`c.Pattern()` 是 `/sitemap.xml`、`/llms.txt`、`/llms-full.txt`，或者页面带 `.md` 的路由，比如 `/blog/{slug}.md`（`/` 对应 `/index.md`）。`c.Language()` 对 `.md` 路径是页面的语言，对其他文件是默认语言。在 `OnRequest` 里把未登录的访客送到登录页面的守卫，也会守住这些文件，所以在不该拦的时候要放行。

函数返回的内容里有错误，绝不会让请求失败。出错的页面或 `Exclude` 模式会被略去，文件用其余的内容响应，每个请求为每一种错误记一行 `ERROR` 日志，指出页面（按路径和语言）或模式：

| 错误 | 会怎样 |
| --- | --- |
| 带参数的路由在 `Pages` 里没有页面，也没有被排除，比如第一篇文章之前的博客 | 在你列出它们之前，文件里没有它的页面。排除它可以让这一行日志消失 |
| 不匹配任何路由的 `Path`，比如你改过名的路由的路径 | 页面被略去 |
| 带语言前缀的 `Path`，或不是转义路径的 `Path` | 页面被略去 |
| 页面的路由没有的 `Language`，或在不属于任何带 `_locales` 的文件夹的页面上写了语言 | 页面被略去 |
| `Alternates` 里有页面没有的语言、同一种语言出现两次，或不是转义路径的路径 | 页面被略去 |
| 同一种语言里同一个页面被列了两次 | 保留第一个 |
| 被列出的页面同时被 `Exclude` 略去 | `Exclude` 优先 |
| 不是集合条目的 `Content`，比如就地写的 `content.Entry{ID: "a"}` | 页面保留，但没有 Markdown 版本 |
| 既不是页面、下面也没有页面的 `Exclude` 模式 | 被忽略 |

函数自己返回的错误，比如查询失败，是这个请求的意外错误，和处理函数里一样：`OnError` 会拿到它，响应是 500。

## 测试它 {#test-it}

`tifatest.Start` 运行整个应用，包括 `OnStart`，所以测试可以像搜索引擎那样读 sitemap（取自 [examples/shop](../../examples/shop/sitemap_test.go)）：

```go
func TestSitemapListsTheStorefront(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	res, err := srv.Client().Get(srv.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), "<loc>https://shop.example.com/store/journal/kettle-photos</loc>") {
		t.Errorf("GET /sitemap.xml: %d %s", res.StatusCode, body)
	}
}
```

函数的错误会让测试以 500 失败。它返回内容里的错误会出现在测试的日志里，以及缺失的 `<loc>` 上。用 `tifatest` 设置的条目，它的正文是一个占位符（见 [测试](../pages-and-data/content.zh-CN.md#tests)），它的 Markdown 版本里也是占位符。

## Tifa 不做的事 {#what-tifa-does-not-do}

- 它不会通知搜索引擎 sitemap 变了。把 `https://<your site>/sitemap.xml` 在 Google Search Console 和 Bing Webmaster Tools 里提交一次，之后它们会自己重新读取。
- 没有 `<changefreq>` 或 `<priority>`：搜索引擎会忽略它们。也没有图片或视频的 sitemap。
- 不替你写 `noindex`：写在需要它的页面的 head 里。
- 对没有 `Content` 的页面，页面的 HTML 仍然是唯一的版本。Tifa 不会把渲染出来的页面转成 Markdown。
