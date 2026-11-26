---
summary: '测试应用的页面：用 Vitest 和 Testing Library 测页面组件（替换 ./route.gen），以及用 Playwright 对着 tifatest.StartPages 在真实浏览器里测页面'
audience: app
order: 15
translatedFrom: bee92638e2e3
read_when:
  - '用 Vitest 和 Testing Library 测试页面组件，或者给页面喂一份 Load 会返回的数据'
  - '用 Playwright 在浏览器里测试页面，或者让 Playwright 对着 Go 测试启动的应用运行'
  - '页面的测试失败，报 `No "createSerializationAdapter" export`，或者找不到 `./route.gen`'
---

# 测试页面 {#testing-pages}

[测试](testing-apps.zh-CN.md)讲的是 Go 测试。这一篇讲 TSX 这一侧：单独在 Node 里测一个组件，以及在浏览器里测一个页面。

## 页面组件 {#a-page-component}

页面是一个 React 组件，所以对它的测试在 Node 里用 Vitest 和 Testing Library 运行，不会启动 Go。数据通过 `./route.gen` 进来：页面用 `page.useLoaderData()` 读数据，用 `POST` 提交表单，两者都来自那个文件，所以测试用 `vi.mock` 把这个文件换掉，给页面一份 `Load` 会返回的数据。把这些包作为开发依赖安装：

```sh
pnpm add -D -E vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom
```

`vitest.config.ts` 沿用应用的 Vite 配置，所以 `tifa/client` 的 import 和别名与构建时一样起作用，再加上测试设置：

```ts
import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.ts'

export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'jsdom',
      include: ['app/**/*.test.tsx'],
      setupFiles: ['./vitest.setup.ts'],
    },
  }),
)
```

`vitest.setup.ts` 加上断言方法，并在每个测试之后清理：

```ts
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)
```

测试放在页面旁边，`app/page.test.tsx`。这一个是给 `tifa new` 写出的应用的首页写的：

```tsx
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TifaError } from 'tifa/client'
import { beforeEach, expect, test, vi } from 'vitest'
import TodosPage from './page'
import { POST, type PageData } from './route.gen'

const loaded: { data: PageData } = { data: [] }
vi.mock('./route.gen', () => ({
  page: { useLoaderData: () => loaded.data },
  POST: vi.fn(),
}))
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
  useRouter: () => ({ invalidate: vi.fn() }),
}))

beforeEach(() => {
  vi.mocked(POST).mockReset()
})

test('shows the todos the Load returned', () => {
  loaded.data = [
    { id: 1, title: 'Buy oat milk', done: false },
    { id: 2, title: 'Write tests', done: true },
  ]
  render(<TodosPage />)
  expect(screen.getByText('Buy oat milk')).toBeInTheDocument()
  expect(screen.getByText('1 open')).toBeInTheDocument()
})

test('shows the message the Go Validate returned', async () => {
  loaded.data = []
  vi.mocked(POST).mockRejectedValue(new TifaError(422, 'Unprocessable Entity', { title: 'Write what needs doing.' }))
  render(<TodosPage />)
  await userEvent.click(screen.getByRole('button', { name: 'Add' }))
  expect(await screen.findByText('Write what needs doing.')).toBeInTheDocument()
})
```

用 `pnpm exec vitest run` 运行。

- `PageData` 是 Go `Load` 返回内容的类型，所以一份不再符合 Go 结构体的测试数据会让 `tsc` 和 `tifa check` 失败（它们也检查测试文件）。改了 Go 类型之后，像对待任何页面一样运行 `go tool tifa gen`。
- 用到 `page.useParams()`、`page.useSearch()` 或 `layout.useLoaderData()` 的页面，在 mock 里也要有它们：把函数加到替换 `./route.gen` 的那个对象里。
- 对 `@tanstack/react-router` 的 mock 保留真实的模块，只替换 `Link` 和 `useRouter`，它们需要外面有一个路由器。`tifa/client` 也会加载路由器模块，所以不带 `importOriginal` 的 mock 会失败，报 `No "createSerializationAdapter" export`。
- 失败的 `POST` 在页面里会是一个带 `status`、`message` 和 `fields` 的 `TifaError`，和在浏览器里一样。在测试里造一个，就能试页面怎样显示 `tifa.Fields` 的错误。
- 这个测试检查的是组件，不是 Go：`Validate` 是否返回那条消息，属于 Go 测试。页面和服务器怎样配合，见[浏览器里的页面](#a-page-in-a-browser)。

## 浏览器里的页面 {#a-page-in-a-browser}

用 Playwright 写的测试会在真实的浏览器里打开页面，所以它覆盖水合、点击、对 Go 处理函数的调用和屏幕上的结果。Go 测试用 `tifatest.StartPages` 启动应用，它会带着 `.tifa/dist` 里的前端提供页面，再把地址交给 Playwright。Playwright 只需要装一次，并先构建 `StartPages` 要读的前端：

```sh
pnpm add -D -E @playwright/test @types/node
pnpm exec playwright install chromium
go tool tifa build
```

`playwright.config.ts` 从环境里取地址。`tifatest` 的服务器用的是自己的证书，走 HTTPS，所以浏览器不能去校验它：

```ts
import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  use: { baseURL: process.env.BASE_URL, ignoreHTTPSErrors: true },
})
```

`e2e/todos.spec.ts` 写的是访客做的事：

```ts
import { expect, test } from '@playwright/test'

test('adds a todo and shows the message of a bad one', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Nothing to do.')).toBeVisible()

  await page.getByRole('button', { name: 'Add' }).click()
  await expect(page.getByText('Write what needs doing.')).toBeVisible()

  await page.getByLabel('New todo').fill('Buy oat milk')
  await page.getByRole('button', { name: 'Add' }).click()
  await expect(page.getByRole('link', { name: 'Buy oat milk' })).toBeVisible()
  await expect(page.getByText('1 open')).toBeVisible()
})
```

Go 这一侧是它自己的一个测试，`app/browser_test.go`。它把数据库指向一个空文件，启动应用，再带着地址运行 Playwright：

```go
package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liustack/tifa/tifatest"

	"demo"
	"demo/.tifa/tifaapp"
)

func TestInBrowser(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("starts a browser: set E2E=1")
	}
	demo.DBPath = filepath.Join(t.TempDir(), "test.db")
	srv := tifatest.StartPages(t, tifaapp.App)

	cmd := exec.Command("pnpm", "exec", "playwright", "test")
	cmd.Dir = ".." // the folder of package.json
	cmd.Env = append(os.Environ(), "BASE_URL="+srv.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("playwright: %v\n%s", err, out)
	}
}
```

用 `E2E=1 go test ./app -run TestInBrowser` 运行。Playwright 会在测试输出里打印失败的地方。它还会写出 `test-results/` 和 `playwright-report/`，这两个应该放进 `.gitignore`。

- 测试用 `E2E=1` 当开关，所以 `go test ./...` 和 `tifa check` 不需要浏览器。想让它到处都跑，就去掉这个开关。
- 自上次 `tifa build` 以来，只要页面、`package.json` 或前端用到的其他文件变了，`StartPages` 就会让测试失败。运行之前再构建一次，或者让 `tifa dev` 一直运行。见[测试整个请求](testing-apps.zh-CN.md#testing-a-whole-request)。
- 一个测试二进制里同一时间只运行一个应用，所以把浏览器测试放在一个 Go 测试里，规格可以分在一个或几个文件里，并且不要调用 `t.Parallel`。数据库是这个测试的空文件，所以每次运行都从零开始。需要有数据的规格，像示例那样通过页面把数据加进去。
- 服务器只在这台机器上监听，只在 Go 测试运行期间存在。没有它 Playwright 启动不了，所以通过 Go 测试来运行规格，不要单独运行 `pnpm exec playwright test`。
