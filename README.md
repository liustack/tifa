# tifa

用 Go 写后端、React 写页面，整个网站编译成一个可执行文件，拷到一台小 VPS 上直接运行。线上不需要 Node，不需要进程管理器、反向代理和 Redis。

适合这样的 Go 开发者：业务逻辑和数据都在 Go 里，页面想用 React 生态来写，又不想为了服务端渲染在生产环境多养一个 Node 服务。

## 写起来是什么样

每个页面一个目录，Go 负责取数，React 负责显示：

```go
// app/projects/orders/detail/route.go
const Path = "/projects/{projectId}/orders/{orderId}"

func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.Get(c.Param("projectId"), c.Param("orderId"))
}
```

```tsx
// app/projects/orders/detail/route.tsx
import { route } from './+route'

export default function OrderPage() {
  const order = route.useLoaderData() // 类型由 Go 的返回值生成
  return <h2>{order.id}</h2>
}
```

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build   # 得到 dist/<应用名>，前端产物已经嵌在里面
```

## 它怎么工作

- 请求进来，Go 先跑完页面和各层 Layout 的 `Load`，再在进程内用纯 Go 的 QuickJS 把 React 渲染成完整 HTML。渲染期间没有 I/O。
- 浏览器由 TanStack Router 接管。之后的跳转只向 Go 要数据，一次跳转一个请求。
- 纯 Go，`CGO_ENABLED=0`，可以交叉编译。构建用 esbuild 的 Go API，不需要 Node，Node 只用来装 npm 包和做类型检查。
- 构建时做静态分析：路径重复、Layout 读了不属于它的参数、`Load` 返回值不能转成 TypeScript 等问题，在构建时报出文件和行号。

代价也说在前面：单次渲染比 Node 慢，订单详情这样的页面在 Apple M4 上热渲染中位数约 5.7 ms。tifa 用预热的 VM 池、按数据缓存 HTML 和 ETag 来弥补，不做流式渲染，也没有 React Server Components。

## 从哪开始

- [examples/shop](examples/shop)：完整的示例应用，店铺、订单、发货、三种 404。
- [docs/app.md](docs/app.md)：应用怎么写，目录、路由、Layout、钩子、404、缓存。
- [docs/build.md](docs/build.md)：`tifa build` 生成了什么，生成的程序怎么启动、怎么回答请求。
- [docs/dev.md](docs/dev.md)：`tifa dev`，改代码后自动重建。
- [docs/testing.md](docs/testing.md)：给参与开发的人，测试与验证。

tifa 还在早期，接口会变。
