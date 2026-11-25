---
summary: 'Tifa 为什么是这样：文件夹就是 URL，每次写入一个地址，没有藏起来的东西'
audience: app
order: 3
translatedFrom: 1505af9e42a1
read_when:
  - '新建 Tifa 应用，想知道它为什么是这样的布局'
  - '在新增一层、一个包或一个辅助函数之前，对照 Tifa 应用应有的样子权衡'
  - '想找路由分组、server functions 或你熟悉的框架里的别的功能，想知道 Tifa 为什么没有'
  - '修改 Tifa 的约定、它的文档、examples/shop 或 `tifa new` 的模板'
---

# 设计理念 {#philosophy}

Tifa 应用就是一个 Go 程序，它的文件夹树就是它的地图：`app/` 下的一个文件夹同时是业务的一部分、URL 的一段和一个 Go 包。本页的几条理念都是为了守住这一点：简单，按业务组织，没有藏起来的东西。每一条讲 Tifa 怎么做、为什么，以及你能得到什么。最后一节列出 Tifa 因此不做的东西和代替的写法。各个文件放在哪、怎么搭起来，见[应用的结构](structure.zh-CN.md)。

## 路径就是 URL {#the-path-is-the-url}

`app/` 下的一个文件夹是 URL 的一段，除此之外没有别的东西能决定 URL：`app/projects/project+/orders/` 就是 `/projects/{project}/orders`。没有哪个文件夹不出现在 URL 里，也没有哪一段 URL 不是来自文件夹。所以 Tifa 没有路由分组，没有登记路由的文件，也没有由构建编出地址的处理函数。参数也是文件夹（`project+`），因为文件夹同时是 Go 包，包名能用哪些字符由 Go 决定。

你能得到：

- 文件夹树展示了应用的每一个 URL，也就是每一个入口。你和 coding agent 要读的只有这一棵树。
- 给文件夹改名，URL 和包名一起改，哪里还在用旧名字，Go 编译器和 TypeScript 检查会找出来。
- 布局和别的文件一样是文件夹里的一个文件，所以一个外框包住哪些页面，看它放在哪就知道。页面不靠路由分组怎么共用外框，见[代替路由分组的写法](../pages-and-data/layouts.zh-CN.md#instead-of-route-groups)。

## 按业务域组织代码 {#group-code-by-domain}

代码按它讲的是什么（订单、项目、客户）来分组，不按代码的类型来分。这一点参考了[按组件组织](https://developer20.com/how-to-structure-go-code/)：一个组件自带它需要的一切，包括存储、HTTP 处理和业务模型。在 Tifa 里，`app/` 下的文件夹本来就是 URL，而 URL 本来就跟着业务走，所以一个业务域就是 `app/` 下的一个文件夹，代码和展示它的页面放在一起。不会有哪个包把各个业务域里同一类代码收到一起。

你能得到：

- 可以把一个业务域整个拿出来、评审或删除。改某个功能时，从它的文件夹入手，大部分改动也留在里面。伸到外面的部分会表现为一条 import，或共享 schema 里的一张表，所以看得见。
- 浏览器里的 URL 就告诉你代码在哪。
- Go 不允许循环 import，所以两个业务域不会互相依赖：业务域之间的 import 永远是单向的。

## 每次写入一个地址 {#one-address-for-each-write}

应用接受的每一种写入都有自己的地址：一个方法加一个路径，路径就是文件夹。改某个东西的字段，是它所在文件夹的 `PATCH`。在它下面新建一项，是放这类子项的文件夹的 `POST`。有自己名字的操作，比如给订单发货，是一个以这个名字命名的文件夹和它的 `POST`。没有让浏览器按名字调用的函数，也没有第二种到达处理函数的办法。三种形状见[一次写入放在哪](../pages-and-data/handlers.zh-CN.md#where-a-write-goes)。

你能得到：

- `OnRequest`、代理、限流和日志只看方法和路径，就知道一个请求是哪种写入，所以谁能做什么在一个地方定。
- `tifa routes` 打印应用的每一条路由，所有入口都读得到。
- 一件事只有一种写法。没有人需要做选择，别人写的、coding agent 写的代码和你写的长得一样。
- 给一个操作开一个文件夹，会让你说清这个操作是什么：它的名字、它的输入、谁能做、什么时候能做。应用长大时，业务也跟着理清了。

## 没有你没写的事 {#nothing-happens-that-you-did-not-write}

Tifa 照代码说的做，不靠猜。它需要知道的，从你写下的名字里读：一个文件夹、一个函数或一个常量。它没法知道的，留给你。

- **写入之后不会自己重新加载页面数据。** Tifa 不知道一次写入改了什么，所以页面需要显示变化时自己调用 `router.invalidate()`：见[从浏览器调用方法处理函数](../pages-and-data/handlers.zh-CN.md#calling-method-handlers-from-the-browser)。
- **不是路由的东西，外面调不到。** 代码能从外面到达，只因为它是某个 `route.go` 的 `Load`、`Layout` 或方法处理函数，不会因为它被导出或在哪里被导入。
- **`OnRequest` 是一个函数，到路由的每个请求都经过它**，页面、导航数据请求和 API 路由都一样：见 [Hooks](../pages-and-data/hooks.zh-CN.md)。
- **一条命令不管谁来跑，做的事都一样**，人、脚本或 coding agent。不一样的只有你传的参数。
- **构建只读你的代码，从不运行它**，见[按 Go 的方式做事](#do-things-the-go-way)。

你能得到：

- 读 `OnRequest` 和请求去的那个文件夹，就知道一个请求会做什么。
- 应用更容易守住安全，因为每个入口都看得见：它是 `tifa routes` 会打印出来的一个文件夹，它有代理、限流和日志都分得开的方法和路径，它读的是 JSON 或表单，任何工具都能发送和检查。
- 没有哪个请求是靠猜发出去的，所以服务器的负载就是页面要的那些。

## 手工连接依赖 {#wire-dependencies-by-hand}

依赖就是普通的 Go 值：包级变量和函数，在 `OnStart` 里设置，在 `OnShutdown` 里释放。没有依赖注入容器，没有反射，也没有替你连接依赖的生成代码。

你能得到：

- 跳转到定义，总是落在真正运行的代码上。
- 什么时候启动什么、先后顺序如何，就是 `OnStart` 的代码，从上往下读。
- 启动时没有任何东西按名字或类型去查找，所以用了没声明的资源会直接编译报错。已声明但 `OnStart` 还没设置的，运行时仍是 nil。
- 除了 Go 本身，没有别的要学。

## 从小处开始 {#start-small}

一个业务域从一个文件开始。有代码需要放的时候，才多出文件或包。有第二个实现需要藏在后面的时候，才引入接口之类的抽象。具体做法见[应用的结构](structure.zh-CN.md#when-a-domain-grows)。

你能得到：

- 在有代码可放之前，不用维护空的分层。
- 结构跟着代码一起长，而不是照着第一天定下的计划走。

## 按 Go 的方式做事 {#do-things-the-go-way}

- **少魔法。** 约定只有文件夹名和少数几个函数名、常量名。其余都是普通的 Go，Tifa 生成的代码也是你能读懂的 Go 和 TypeScript。
- **构建只读你的代码。** `tifa build` 分析源码，从不运行它：应用的 `init()` 在构建期间不会执行。
- **平台差异用 Go 的构建约束。** 构建按 go 命令的方式，为目标平台挑选文件。
- **纯 Go。** 应用用 `CGO_ENABLED=0` 构建成一个可执行文件。

你能得到：

- `go test ./...`、`go vet ./...` 和你的编辑器，都能像对待任何 Go 模块一样对待这个应用。
- 构建可以放心在任何地方运行（包括 CI），不需要应用运行时才用的数据库或密钥。
- 应用构建成一个可以直接复制的文件，支持它的引擎 [beastjs](https://github.com/liustack/beastjs) 所支持的每个平台：amd64 和 arm64 上的 linux 与 darwin，以及 amd64 和 arm64 上的 windows。

## Tifa 不做什么 {#what-tifa-leaves-out}

下面每一样都掂量过，然后没做，为的是一件事只留一种写法。

| 你可能在找 | 这样写 |
| --- | --- |
| `(shop)` 这样的路由分组 | `layout_.tsx`、URL 前缀，或几个布局渲染同一个外框组件：[代替路由分组的写法](../pages-and-data/layouts.zh-CN.md#instead-of-route-groups) |
| server functions、server actions 或命名动作 | 每个操作一个文件夹，里面一个 `POST`：[一次写入放在哪](../pages-and-data/handlers.zh-CN.md#where-a-write-goes) |
| 每次写入后自动重新加载的页面数据 | 页面需要时调用 `router.invalidate()`：[从浏览器调用方法处理函数](../pages-and-data/handlers.zh-CN.md#calling-method-handlers-from-the-browser) |
| 写在组件里的 `<title>` 和 `<meta>` | `head` 导出：[标题和 meta 标签](../pages-and-data/head.zh-CN.md) |
| 静态生成和预渲染 | 带 `Cache` 的页面：[没有静态生成](app.zh-CN.md#no-static-generation) |
| 依赖注入容器 | 在 `OnStart` 里设置的包级变量：[手工连接依赖](#wire-dependencies-by-hand) |
| 定时任务的 API | 在 `OnStart` 里启动的 goroutine：[后台任务](../guides/jobs.zh-CN.md) |
