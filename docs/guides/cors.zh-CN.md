---
summary: '让另一个网站上的页面调用 Tifa 应用的 API：在 OnRequest 里回答 OPTIONS 预检，设置 Access-Control-Allow-*，以及它和 server.trusted_origins 的关系'
audience: app
order: 17
translatedFrom: f897ab2f5db5
read_when:
  - '另一个前端或合作方网站在浏览器里调用应用的 API 路由，控制台说「blocked by CORS policy」，或者调用以「Failed to fetch」失败'
  - '回答 OPTIONS 请求，或者设置 `Access-Control-Allow-Origin`'
  - '跨站 POST 得到 403 `cross-origin request detected`，不知道该用 `server.trusted_origins` 还是 CORS 头'
---

# 来自另一个网站的调用（CORS） {#calls-from-another-site-cors}

你自己的页面调用你自己的路由，是同源的，这里的内容都用不上。这篇讲的是**另一个源**上的页面，比如 `https://app.example.com` 在浏览器里调用 `https://api.example.com/api/items`。来自服务器、curl 或 App 的调用不受 CORS 限制。

Tifa 不发 CORS 头，也没有相关的设置。它是 `OnRequest` 里的几行代码。

## 两道关，两处设置 {#two-gates-two-settings}

来自另一个源的浏览器调用要过两道检查，各有各的开法：

| 检查 | 谁来做 | 怎么放行 |
| --- | --- | --- |
| **CORS**：页面能不能发这个请求并读到回答？ | 浏览器，依据 `Access-Control-Allow-*` 头 | 你的 `OnRequest`，见下 |
| **跨站写入**：来自另一个网站的 POST、PUT、PATCH 或 DELETE 到底能不能运行？ | 程序，在 `OnRequest` 之前（[跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes)） | `server.trusted_origins`，或者对某个路径用 `server.cross_site_paths` |

读（GET）只需要 CORS。写两个都要。对另一个源上的页面：

| 设置 | GET | 带 JSON 正文的 POST |
| --- | --- | --- |
| 都没有 | 被拦 | 被拦 |
| 给这个源的 CORS 头 | 200 | 被拦，程序回答了 403 |
| CORS 头加 `server.trusted_origins` | 200 | 200 |

被拒绝的写入是程序在 `OnRequest` 之前回答的，所以不带 CORS 头，页面只看到「Failed to fetch」，看不到 403。请看程序的日志：它会打印 `refused a cross-site write`，带上它看到的 `Origin`。

带 JSON 正文的 POST 不是简单请求，所以浏览器先发一个 OPTIONS 预检。预检从不按写入来检查，而且 `OnRequest` 会为它运行。

## 路由没有 OPTIONS {#a-route-has-no-options}

`route.go` 接受 `GET`、`POST`、`PUT`、`PATCH` 和 `DELETE`（[约定](../reference/conventions.zh-CN.md)）。里面一个叫 `OPTIONS` 的函数不是路由方法：`tifa routes` 对这样的路由只列出 `GET` 和 `POST`，`tifa check` 也不会对这个函数发警告。没有钩子时，OPTIONS 请求得到 405 和 `Allow: GET, HEAD, POST`。所以要在 `OnRequest` 里回答预检，它最先看到每一个请求。

## 钩子 {#the-hook}

根包里的 `cors.go`，以及放允许的源的选项：

```go
// CORSOrigins are the origins of other sites whose pages may call the API under /api/, such as
// https://app.example.com. Empty, the default, allows none.
var CORSOrigins = tifa.Strings("cors.origins", nil, "Origins of other sites that may call /api/ from a browser")

```

```go
package cors

import (
	"net/http"
	"slices"
	"strings"

	"github.com/liustack/tifa"
)

// corsPaths is the part of the site that other origins may call.
const corsPaths = "/api/"

// allowCORS answers the preflight of an API request, and adds the CORS headers to the answer of
// the others. It returns true when it wrote the answer. OnRequest calls it first.
func allowCORS(c *tifa.Ctx) (bool, error) {
	r := c.Request()
	origin := r.Header.Get("Origin")
	if origin == "" || !strings.HasPrefix(c.Path(), corsPaths) {
		return false, nil
	}
	// The answer depends on Origin, so a cache must keep one copy for each.
	c.Writer().Header().Add("Vary", "Origin")
	if !slices.Contains(CORSOrigins, origin) {
		return false, nil // no CORS headers: the browser does not let the page read the answer
	}
	c.SetHeader("Access-Control-Allow-Origin", origin)
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		// A preflight: the browser asks before it sends a request that is not simple.
		c.SetHeader("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
		c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.SetHeader("Access-Control-Max-Age", "600")
		return true, c.NoContent(http.StatusNoContent)
	}
	return false, nil
}

```

```go
package cors

import (
	"context"

	"github.com/liustack/tifa"
)

func OnStart(ctx context.Context) error { return nil }

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if done, err := allowCORS(c); done {
		return err
	}
	return next(c)
}

```

- **一份源的清单，按文本比较。** 浏览器发的 `Origin` 是协议、主机和端口，清单里放同样的文本。不要把收到的 `Origin` 原样回显：那等于允许所有网站。不在清单里的源的页面得不到 CORS 头，所以浏览器不让它读回答。
- **`Vary: Origin`**，对每个带 `Origin` 的调用都加上，这样缓存不会把给某个源做的回答交给另一个源。用的是添加而不是设置，这样 Tifa 的 `Vary: Accept-Encoding` 还在。
- **只有 `/api/`。** 应用的页面和导航数据不是给其他源的。
- **预检用 204 回答**，不会到达路由，所以 `Validate` 和 `Load` 都不会为它运行。`Access-Control-Allow-Headers` 列出页面在简单头之外可以发的头：JSON 要 `Content-Type`，令牌要 `Authorization`。`Access-Control-Max-Age` 让浏览器把这个回答记十分钟。
- **没有凭据。** 代码不发 `Access-Control-Allow-Credentials`，所以浏览器既不会随这些调用发 cookie，也不会让页面读为 cookie 做的回答。Tifa 的会话 cookie 是 `SameSite=Lax`（[cookie](../pages-and-data/sessions.zh-CN.md#the-cookie)），不是另一个网站的页面该依赖的东西。给这样的客户端一个放在 `Authorization` 里的令牌，在 Go 里检查它。带 cookie 从另一个源调用，这里不讲。

对没有 cookie 也没有令牌的公开只读 API，另一个选择是 `Access-Control-Allow-Origin: *`，那样就不需要清单了。

## 放行写入 {#allow-the-writes}

`cors.origins` 里可以写入的源，把同样的源也列进 `server.trusted_origins`：

```yaml
# tifa.yaml
server:
  trusted_origins: "https://app.example.com"
```

只信任你自己控制的网站，因为受信任的源上的页面可以替你的用户做事。如果 API 用的是 bearer 令牌，不用 cookie，而且任何网站都可以调用，那么用 `server.cross_site_paths` 配上那个路径的模式，就能让所有源写入它，这只在那里没有任何东西依赖 cookie 时才安全。这两个设置，以及源的准确写法，见 [跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes)。

## 测试 {#test-it}

用 `tifatest.Start` 启动应用，检查：

- 来自清单中的源的预检得到 204，带上这个源、方法和 `Vary: Origin`
- 来自另一个源的预检得到 405，没有 CORS 头，`/api/` 之外的路径也一样
- 来自清单中的源的 GET 带 `Access-Control-Allow-Origin`
- 跨站 POST 得到 403，直到这个源进了 `server.trusted_origins`

手动对构建出的程序试一下：

```sh
curl -i -X OPTIONS http://127.0.0.1:9527/api/items \
  -H 'Origin: https://app.example.com' -H 'Access-Control-Request-Method: POST'
```

回答是 `204`，带 `Access-Control-Allow-*` 头。去掉 `Origin`，你得到的就是任何 OPTIONS 都会得到的 405。
