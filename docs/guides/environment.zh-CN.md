---
summary: '环境里的值怎样到达页面：Load、`import.meta.env.VITE_*`、`process.env`'
audience: app
order: 10
translatedFrom: d4c2aa469f11
read_when:
  - '你来自 Next.js 或 Vite，在页面里习惯性地用 `import.meta.env.VITE_*`、`NEXT_PUBLIC_*` 或 `process.env`'
  - '构建警告说 `import.meta.env.VITE_*` 在构建时就固定了'
  - '页面需要公钥、API 基础地址或在不同环境下不同的功能开关'
  - '决定密钥放在哪里'
---

# 来自环境的值 {#values-from-the-environment}

在 Tifa 里，环境和密钥都留在 Go 进程里。程序启动时读取它的设置：命令行参数、环境变量、`.env` 和 `-c` 指定的文件，Tifa 自己的设置还可以写在 `tifa.yaml` 里（见[配置](../build-and-deploy/config.zh-CN.md)）。前端构建看不到这些。Vite 不读 `.env`（`envDir: false`），而且同一个应用的构建要能在每个环境里运行。

想把一个值交给浏览器，就从 `Load` 返回它，或者从 `Layout` 返回，这样它下面的每个页面都能拿到。这行 Go 是值到达页面的唯一途径，所以一定是你有意写的，也很容易找到。

## 示例 {#an-example}

先和应用的设置一起定义选项（见[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)）：

```go
// options.go
var PublicKey = tifa.String("stripe.public_key", "", "Public key the browser uses")
```

在程序运行的地方设置它：`STRIPE_PUBLIC_KEY=pk_live_... ./bin/app`，或者 `.env` 里的一行，或者 `-c` 指定的文件，或者 `--stripe-public-key`。从 `Load` 返回它：

```go
// app/checkout/route.go
type Page struct {
	PublicKey string `json:"publicKey"`
}

func Load(c *tifa.Ctx) (Page, error) { return Page{PublicKey: shop.PublicKey}, nil }
```

在页面里读取：

```tsx
// app/checkout/page.tsx
import { page } from './route.gen'

export default function Checkout() {
  const { publicKey } = page.useLoaderData()
  return <PayForm publicKey={publicKey} />
}
```

同一份构建，现在在预发环境用测试密钥，在生产环境用正式密钥。

## 为什么不用名字前缀 {#why-not-a-name-prefix}

在 Vite 和 Next.js 里，名字前缀（`VITE_`、`NEXT_PUBLIC_`）决定一个值会不会进入浏览器拿到的 JavaScript，而且这发生在构建运行的时候。换一个新值就要重新构建，给密钥写错前缀，则会把它发给每个访问者。在 Tifa 里，没有任何名字会这样做。不是从 `Load` 返回的值，就不会出现在页面里。

`Load` 返回的内容，每个访问者都看得到。永远不要返回密钥，比如 `tifa.Secret`、密码或令牌。

## `import.meta.env` 还有什么用 {#what-importmetaenv-is-still-for}

Vite 仍然会用下面这些值填充 `import.meta.env`：

- `MODE`、`DEV` 和 `PROD`：在 `tifa build` 里，无论是浏览器包还是服务端渲染，分别是 `production`、`false` 和 `true`。在 `tifa dev` 里，无论是浏览器模块还是服务端渲染，分别是 `development`、`true` 和 `false`。
- `SSR`：在服务端渲染里是 `true`，在浏览器里是 `false`。
- `BASE_URL`：在 `tifa build` 里是 `/`，在 `tifa dev` 里是 `/__tifa/vite/`（浏览器模块由 Vite 的开发服务器提供，服务端渲染里也是这个值）。

`tifa dev` 的服务端渲染仍然运行 React 和其他库的生产版本：那里的 `process.env.NODE_ENV` 是 `production`。

Vite 还会把运行 `tifa build` 的 shell 环境里每个以 `VITE_` 开头的变量，以及应用在 `vite.config.ts` 里通过 `define` 给出的每个 `import.meta.env.*` 常量，写进包里。这些值在构建运行时就固定了。它们适合在构建运行的每个地方都相同的值，不适合随环境变化的值。

当应用代码读取 `import.meta.env.VITE_*` 时，`tifa build` 和 `tifa dev` 会给出警告：每个名字警告一次，并指出第一次读取的位置。在 `tifa dev` 下，这个警告也会出现在 `go tool tifa errors` 的 `Warnings` 下，直到有一次构建里不再有它。警告覆盖 `import.meta.env.VITE_X`、`import.meta.env['VITE_X']` 和 `const { VITE_X } = import.meta.env`。不覆盖把 `import.meta.env` 换个名字传下去的写法。`node_modules` 里的代码不检查。
