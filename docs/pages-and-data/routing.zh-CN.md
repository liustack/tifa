---
summary: '文件夹怎样变成 URL：参数、重叠的路由、一个文件夹里的文件'
audience: app
order: 1
translatedFrom: 63e560a47f34
read_when:
  - '决定一个路由放进哪个文件夹，或者构建拒绝了某个文件夹名或两个重叠的路由'
  - '给路由加参数，或者在 Go 里用 `c.Param`、在 TypeScript 里用 `Link` 和 `params` 读取它'
  - '决定一个文件夹需要哪些约定文件，或者只有 `route.go` 的文件夹是不是 API 路由'
---

# 路由 {#routing}

## URL 来自文件夹 {#urls-come-from-folders}

`app/` 下的每个文件夹都是 URL 的一段。`app/` 自己是 `/`。

| 文件夹名 | URL 段 |
| --- | --- |
| `orders` | 字面的 `orders` |
| `order+` | 参数 `{order}`，用 `c.Param("order")` 读取 |
| `path++` | 路径的其余部分 `{path...}`，只能作为最后一个文件夹。它可以是空的：`files/path++` 也响应 `/files`。在 Go 里用 `c.Param("path")` 读取，它在 TypeScript `Params` 里的键是 `_splat` |

- 字面的文件夹名只含 ASCII 字母、数字和 `- . _ ~`。参数名以字母或 `_` 开头，后面是字母、数字或 `_`，并且在一个 URL 里只能出现一次。
- go 命令从 `package` 那一行取包名，不从文件夹取。用参数名给包命名：`project+` 里是 `package project`，别的包用 `project` 导入它。
- go 命令会跳过以 `.` 或 `_` 开头的文件夹，以及名为 `testdata` 的文件夹。里面有 `route.go`、`page.tsx`、`layout.tsx` 或 `layout_.tsx` 会让构建失败。别的文件，比如 `_components` 文件夹，没有问题。
- 文件夹名还必须在 Go 导入路径里有效，所以像 `con` 或 `x~1`（Windows 的保留名和短名）这样的名字会让构建失败。
- 一个文件夹最多只有一个参数文件夹。`items/id+` 旁边有 `items/key+` 或 `items/path++` 会让构建失败，因为两者读的是同一个 URL 段。
- 两个路由（页面或 API 路由）的 URL 重叠，而又没有哪个更具体，会让构建失败，和 Go 的 `http.ServeMux` 一样，比如 `a/b+` 旁边有 `a+/b`。字面值比参数更具体，所以 `items/new` 旁边有 `items/id+` 没有问题。
- 有布局但没有页面的文件夹，它的 URL 会在那个布局里显示 404（见 [404](../pages-and-data/errors.zh-CN.md#404)），除非它正下面的 `name++` 文件夹有页面，那个页面会以空的其余部分响应这个 URL。其他页面不能匹配这个 URL，除非它比布局的路径更具体：`settings/layout.tsx` 没有 `settings/page.tsx`，旁边又有 `user+/page.tsx`，会让构建失败，因为 `/settings` 也会匹配 `/{user}`，但 `items/id+` 里有布局、旁边有 `items/new` 就没有问题。
- 声明 `const Path` 会让构建失败。文件夹就是 URL。

### Go 和 TypeScript 里的参数名 {#parameter-names-in-go-and-typescript}

一个参数在文件夹里有一个名字，在每种语言里有各自的写法。以 `app/projects/project+` 和 `app/files/path++` 为例：

| 文件夹 | Go：`c.Pattern()` 和 `tifa routes` | TypeScript：`Link to` | TypeScript：`params` 的键 |
| --- | --- | --- | --- |
| `project+` | `/projects/{project}` | `/projects/$project` | `project` |
| `path++` | `/files/{path...}` | `/files/$` | `_splat` |

`c.Param("project")` 和 `c.Param("path")` 在 Go 里按文件夹的名字读取它们。

## 一个文件夹里的文件 {#the-files-of-a-folder}

一个文件夹最多有三个约定文件：

| 文件 | 它放什么 |
| --- | --- |
| `route.go` | 这个文件夹所有的 Go 名字：`Layout`、`Load`、`SSR`、`Memo`、`Cache` 和方法处理函数 |
| `page.tsx` | 页面组件。有 `page.tsx` 的文件夹就是一个页面 |
| `layout.tsx` | 布局组件 |
| `layout_.tsx` | 代替 `layout.tsx` 的布局组件，显示时不带上面的布局（见 [不带上层布局的布局](../pages-and-data/layouts.zh-CN.md#a-layout-without-the-layouts-above)） |

- `route.go` 里的这些名字只从那个文件里读。在包里的其他文件里声明其中任何一个，包括 `layout.go`，都会让构建失败。辅助函数和类型可以放在任何文件里。
- 只有 `route.go` 的文件夹，在声明了方法处理函数时是一个 API 路由，在声明了 `Layout` 时给它下面的页面提供布局数据。两者可以同时有。`Load` 只允许和 `page.tsx` 放在一起。
- `tifa build` 还会为每个有页面、布局或方法处理函数的文件夹写一个 `route.gen.ts` 模块，`page.tsx`、`layout.tsx` 和 `layout_.tsx` 会导入它（见 [page.tsx](../pages-and-data/loading-data.zh-CN.md#pagetsx)）。
