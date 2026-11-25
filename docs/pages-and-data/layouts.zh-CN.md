---
summary: 'layout.tsx 和 Layout、嵌套、tifa.Use、layout_.tsx、代替路由分组的写法'
audience: app
order: 2
translatedFrom: 9685cf2db789
read_when:
  - '编写 layout.tsx 或 route.go 里的 `Layout` 函数，或者用 `tifa.Use` 共享它的数据'
  - '用 `layout_.tsx` 给站点的一部分单独的外框'
  - '在找路由分组、`(group)` 文件夹，或者不往 URL 里加东西的 Layout'
  - '想让站点的一部分页面有别的页面没有的外框，比如首页旁边有一个控制台'
  - '想知道为什么 `/` 的页面总是显示在根 Layout 里'
---

# 布局 {#layouts}

## 文件夹的布局 {#the-layout-of-a-folder}

布局放在它自己的文件夹里，在该文件夹的页面旁边（如果有页面的话）：`route.go` 里的 `Layout` 函数和 `layout.tsx` 包住同一文件夹里的页面和它下面的每个页面。`layout.tsx` 用 `<Outlet />` 放置子路由。两者都可以省略：没有 `layout.tsx` 时子路由直接渲染，没有 `Layout` 函数时这个布局没有数据。一个在它的文件夹里和它下面都没有页面的布局，会让构建失败。

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
```

- `Layout` 放在 `route.go` 里，与同一文件夹的 `Load`、`SSR`、`Memo`、`Cache` 和方法处理函数放在一起。在包里的任何其他文件里（比如 `layout.go`）声明它，构建都会失败。只有 `Layout` 的 `route.go` 不是 API 路由。
- 布局的 `c.Param` 只能读取它自己文件夹路径里的参数。`app/projects/project+/route.go` 里的 `Layout` 可以读 `project`，不能读 `order`。页面的 `Load` 同样被限制在它自己的路径上。
- 在同一个布局下的页面之间切换时，布局组件不会重新挂载，保持它的状态。当布局函数读取的路径参数变了（比如你转到另一个项目），布局会重新挂载，它的状态重置。查询参数变了时，即使是它读取的，布局也会重新加载它的数据并渲染，但不会重新挂载，保持它的状态，这是 TanStack Router 的默认行为。所以过滤条件会保持访客选的视图。组件根据 `Load` 的数据设置的状态，比如用 `useState(data.x)`，也不会因为新的查询而重置。需要它跟随新数据的地方，给组件一个来自这个值的 `key`，或者在 effect 里更新状态。页面的工作方式相同。
- 页面和布局可以调用 `tifa.Use(c, project.Layout)`，为当前请求拿到包住它们的某个布局的结果：同一文件夹或上层文件夹的布局，除非中间的 `layout_.tsx` 把它去掉了。同一个 `Layout` 在每个请求里只运行一次。任何其他布局都会让构建失败，参数不是包级的 `Layout` 函数（比如闭包）也一样。`Layout` 函数不能使用它自己的布局，不管是直接还是通过辅助函数。
- 在浏览器里导航时，导航数据请求只运行所列路由的 `Load` 和 `Layout` 函数。如果某个上层布局的数据还在缓存里，那个布局就不在列表里，它的 `Layout` 这次不会运行。所以不要只在布局里做权限检查。把它们放进 `OnRequest`，或者让页面的 `Load` 通过 `tifa.Use` 依赖做检查的布局，这样检查在每个请求上都会运行。
- 根布局还放着站点的 `documentHead`（见[文档外壳和 documentHead](head.zh-CN.md#the-document-shell-and-documenthead)）。

## 不带上层布局的布局 {#a-layout-without-the-layouts-above}

把组件文件命名为 `layout_.tsx` 而不是 `layout.tsx`，这个文件夹的布局就会显示它的页面，而不带它上面的任何布局，根布局也包括在内。只有框架写出的文档外壳留在它们周围。[examples/shop](../../examples/shop) 的店面就是这样工作的：它自己画页眉和页脚，根布局给其他页面画的工作台，它一点也没有。

```text
app/
  route.go  layout.tsx                the workbench: sidebar, top bar, the list of shops
  projects/
    project+/
      route.go  layout.tsx  page.tsx  inside the workbench
  store/
    layout_.tsx                       the storefront starts here, without the workbench
    _locales/                         zh-CN.json and en-US.json
    route.go  page.tsx                the storefront
    cart/
      route.go  page.tsx              inside the storefront
```

- 它下面的页面，更深层的也一样，不会渲染它上面的布局，上面那些布局的 `Layout` 函数也不会为它们运行：不为页面，不为浏览器的导航数据请求，也不为它下面没有页面的 URL 的 404。所以它们的数据永远到不了这些页面。
- 同一文件夹 `route.go` 里的 `Layout` 照常给它数据。它下面的文件夹照常嵌套：更深一层的 `layout.tsx` 显示在它里面，它的 `Layout` 会运行。更深一层的 `layout_.tsx` 则依次把它连同它上面的一切都去掉。
- 属于文档的东西仍然适用：根布局的 `documentHead`（连同它的 `htmlAttributes`），以及 `OnRequest`。属于上层布局的则不适用：它们的 `head` 和它们的 `ErrorBoundary`。当 `layout_.tsx` 的 `Layout` 失败，或返回 `tifa.ErrNotFound` 时，它上面没有布局来显示错误，所以显示框架的默认页面。它自己的 `ErrorBoundary` 显示它下面失败的东西，和任何布局一样。要在那个 `Layout` 失败时显示你自己的页面，把 `layout_.tsx` 放高一层文件夹，带上 `<Outlet />` 和 `ErrorBoundary`。
- 在浏览器里，从它下面的页面移到它外面的页面，会换掉整个框架，新页面的布局在路由器还没有它们的数据时加载数据。
- 构建会在这些情况下失败：`app/` 里有 `layout_.tsx`（它上面没有布局），同一文件夹里同时有 `layout.tsx` 和 `layout_.tsx`，以及它下面的页面或布局对被某个 `layout_.tsx` 去掉的布局使用 `tifa.Use`。它也会在一个布局的页面全都在某个 `layout_.tsx` 下面时失败，因为它一个也没有包住。
- React Router 和 TanStack Router 也用 `_` 结尾的名字来让路由不进入某个布局，但只是不进入它前一段的布局：根路由和更上层的布局仍然包着它。Tifa 的 `layout_.tsx` 会去掉它上面的每个布局，根布局也不例外。

## 代替路由分组的写法 {#instead-of-route-groups}

路由分组是这样一种文件夹：它给里面的页面一个布局，却不往 URL 里加东西。Next.js、SvelteKit 和 Nuxt 写成 `(shop)`，TanStack Router 是 `_name` 路由，React Router 是不带路径的布局路由。Tifa 没有路由分组，以后也不会加。这一节讲路由分组的每种用途在 Tifa 里怎么写，以及 Tifa 为什么不要它。

在 Tifa 里，`app/` 下的每个文件夹是一个 URL 段，布局包住它所在文件夹和下面的页面。下文把布局画在页面外面的那一圈叫「外框」：页头、侧栏、页脚。

### 站点的一部分有自己的外框 {#one-part-of-the-site-has-a-frame-of-its-own}

把这一部分的布局文件命名为 `layout_.tsx`。它的页面显示时不带上面的任何布局，根布局也不带。

目录树和规则见[不带上层布局的布局](#a-layout-without-the-layouts-above)。

### 几个页面共用一个外框，可以共用一个 URL 前缀 {#several-pages-share-a-frame-and-can-share-a-url-prefix}

把它们放进同一个文件夹。文件夹就是前缀，它的布局就是外框。

```text
app/
  layout.tsx                    公开页面的外框
  page.tsx                      /
  pricing/
    page.tsx                    /pricing
  console/
    route.go  layout_.tsx       控制台的外框和它的数据
    page.tsx                    /console
    settings/
      route.go  page.tsx        /console/settings
```

在 `/console` 和 `/console/settings` 之间切换时，外框保持挂载，状态还在，它的数据也不会再加载。控制台要显示在根布局里面时，`console/` 里用 `layout.tsx`。不要根布局时用 `layout_.tsx`。

### 几个页面共用一个外框，URL 留在同一层 {#several-pages-share-a-frame-and-their-urls-stay-at-the-same-level}

比如 `/dashboard` 和 `/settings` 共用控制台的外框，旁边的 `/` 和 `/pricing` 没有这个外框，而且不想加前缀。给每个文件夹各写一个布局，用同样的数据渲染同一个组件：

```text
app/
  layout.tsx                    公开页面的外框
  page.tsx                      /
  pricing/
    page.tsx                    /pricing
  dashboard/
    route.go  layout_.tsx  page.tsx
  settings/
    route.go  layout_.tsx  page.tsx
components/
  console-frame.tsx             外框，一个普通组件
console/
  frame.go                      package console：type Frame、func LoadFrame
```

```go
// app/dashboard/route.go，app/settings/route.go 里写同样的函数
package dashboard

func Layout(c *tifa.Ctx) (console.Frame, error) {
	return console.LoadFrame(c.Context())
}
```

```tsx
// app/dashboard/layout_.tsx，app/settings/ 里放同样的文件
import { Outlet } from '@tanstack/react-router'
import { ConsoleFrame } from '@/components/console-frame'
import { layout } from './route.gen'

export default function Layout() {
  return (
    <ConsoleFrame frame={layout.useLoaderData()}>
      <Outlet />
    </ConsoleFrame>
  )
}
```

这样写的代价：`/dashboard` 和 `/settings` 是两个长得一样的布局。访客从一个走到另一个时，外框会重新挂载，它保存的状态会从头开始，比如展开的菜单、列表滚动到的位置，它的数据也会再加载一次，和页面的数据在同一个请求里。在意这个代价时，像上一节那样给这些页面加前缀。

### `/` 的页面 {#the-page-at-}

`app/` 里不能放 `layout_.tsx`，所以 `/` 的页面总是显示在根布局里。让根布局做 `/` 这个页面的外框，其他外框不同的部分用 `layout_.tsx` 离开它，就像上面几棵目录树那样。所有页面都没有共用外框的应用，可以不写根 `layout.tsx`。

### Tifa 为什么没有路由分组 {#why-tifa-has-no-route-groups}

- **路径就是 URL。** `app/` 下的每个文件夹是一个 URL 段，也是一个 Go 包，所以看文件的路径就知道 URL，看 URL 就能找到文件。有了路由分组，一个文件夹可能算一段，也可能不算，两个文件夹还可能对应同一个 URL。
- **写法只能是 Tifa 自己的。** `app/` 下的文件夹是 Go 包，而 `(shop)` 不是合法的 Go 导入路径。Tifa 得用一种别的框架都没有的写法。
- **常见的情况 `layout_.tsx` 已经能做。** 站点的一部分要离开上面的外框，不需要分组。路由分组会是做同一件事的第二种办法。

这件事已经定了，没有加路由分组的计划。名为 `(name)` 的文件夹会让 `tifa check` 和 `tifa build` 失败，并有一行说明。
