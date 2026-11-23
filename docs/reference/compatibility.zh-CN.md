---
summary: 'Tifa v0.1 对应用保持稳定的东西（包、文件约定、生成的路径、命令、设置、HTTP 名称），以及哪些东西在任何版本里都可能改变'
audience: app
order: 3
translatedFrom: 5f64614a074c
read_when:
  - '判断升级 Tifa 会不会弄坏你的应用'
  - '在 Tifa 程序前面放 CDN、代理或监控'
  - '想知道能不能导入 `server` 包，或者解析 `tifa routes` 或 `tifa errors` 的 JSON'
  - '修改 Tifa 的某个公开名称，需要知道是否要升次版本'
---

# 兼容性 {#compatibility}

从 v0.1 起，凡是会破坏下面稳定清单中任何一项的改动，都会放在 minor 版本里发布（`v0.1.x` 到 `v0.2.0`），不会放在补丁版本里。更新日志会写明要改什么，`go tool tifa upgrade` 也会把它打印出来。不在清单上的名称，任何版本都可能改变。

## 稳定的内容 {#stable}

**Go 包。** `github.com/liustack/tifa`、`github.com/liustack/tifa/content` 和 `github.com/liustack/tifa/tifatest` 导出的名称。包括 `hooks.go` 里各个 hook 的名称，`route.go` 里 `Load`、`Layout` 和方法处理函数的签名，`route.go` 会读取的常量（`SSR`、`Memo`、`Cache`），`Validate` 和 `Exposed` 方法，`tifa.Stats()` 的字段，以及[路由](../pages-and-data/routes.zh-CN.md)里的 JSON 规则。

**文件约定。** 根目录的文件（`go.mod`、`tifa.yaml`、`hooks.go`、`setup.ts`、`options.go`、`content.go`、`content/`、`public/`、`app/`），`app/` 里文件夹名的含义（`name`、`name+`、`name++`），文件夹里的约定文件（`route.go`、`page.tsx`、`layout.tsx`、`layout_.tsx`），`page.tsx` 和 `layout.tsx` 导出的内容，`_locales/<language>.json`，以及 `*.client.tsx`。

**应用会 import 的生成路径。** `.tifa/vite.js`、`.tifa/tsconfig.json`、`tifa/client` 和 `tifa/i18n`（由应用的 `tsconfig.json` 映射），`./route.gen` 及其导出，`tifa/client` 的 `SessionData` 类型，以及 Go 包 `<module>/.tifa/tifaapp` 和它的变量 `App`。

**命令和参数。** `go tool tifa` 的各个命令（`new`、`build`、`dev`、`gen`、`check`、`routes`、`errors`、`docs`、`agents`、`upgrade`）及其参数。构建出的程序的 `serve`、`config` 和 `version`，`-c/--config`，以及每个设置对应的一个参数。

**`tifa.yaml` 的键。** [config.md](../build-and-deploy/config.zh-CN.md) 里的每个键，连同它的参数、环境变量、类型和默认值。Tifa 去掉一个键，会在 minor 版本里进行，更新日志会说明该改写成什么。

**应用和 CDN 会碰到的 HTTP 名称。**

- 导航数据请求的查询参数 `_tifa`。放在程序前面的缓存必须把它算进缓存键。
- 路径前缀 `/__tifa/`，包括 `/__tifa/session`，后者不能被缓存。
- 请求头 `X-Tifa-Call`（`1` 表示来自浏览器运行时的调用）。
- 响应头 `X-Tifa-Build`，当浏览器和程序不是同一次构建时，它会随 409 一起返回。
- 响应头 `X-Tifa-Render` 及其四个取值 `vm`、`shared`、`memo` 和 `none`。
- `session.cookie` 的默认值 `__Host-tifa_session`（在 `tifa dev` 下是 `tifa_session`）。

## 任何版本都可能改变的内容 {#may-change-in-any-release}

- `tifa deploy` 及它的全部子命令（`setup`、`rollback`、`env`、`logs`、`status` 和 `remove`）及其参数，setup 打印的计划，`tifa.yaml` 的 `deploy.*` 键，它们在服务器上保存的文件、unit 和记录，`tifa deploy setup` 写入的工作流（`.github/workflows/tifa-deploy.yml`），以及它在 GitHub 和 Cloudflare 里设置的内容。它们还很新，在通过 CI 和 Cloudflare 账号部署的流程稳定下来之前，可能会变。
- `server` 包。它是公开的，只是因为你模块里生成的代码要 import 它。不要 import 它，用 `tifa` 和 `tifatest`。
- `tifa routes --json` 和 `tifa errors --json` 的 JSON。在脚本里读取它们风险自负，或者改用文本输出。
- 其他 `X-Tifa-*` 头（`X-Tifa-Redirect`、`X-Tifa-Session`、`X-Tifa-Dev`、`X-Tifa-Reload`），浏览器运行时和程序之间的 JSON 信封，`Server-Timing` 的细节，浏览器的 `localStorage` 和 `sessionStorage` 键，以及页面的元素 id。同一次构建的浏览器和程序彼此一致，而另一次构建的程序会回答 409，让页面重新加载。
- `internal/` 下的一切，`.tifa/` 的内容（上面列出的路径除外），打包出的 JavaScript，以及日志和错误消息的文字。
- 速度、内存占用，以及页面确切的 HTML 字节。
