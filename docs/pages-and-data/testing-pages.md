---
summary: 'Test the pages of an app: a page component with Vitest and Testing Library (mocking ./route.gen), and a page in a real browser with Playwright against tifatest.StartPages'
audience: app
order: 15
read_when:
  - Testing a page component with Vitest and Testing Library, or feeding a page the data a Load would return
  - Testing a page in a browser with Playwright, or running Playwright against the app that a Go test started
  - A test of a page fails with `No "createSerializationAdapter" export` or cannot find `./route.gen`
---

# Testing pages

[Testing](testing-apps.md) is about Go tests. This page is about the TSX side: a component alone in Node, and a page in a browser.

## A page component

A page is a React component, so a test of it runs in Node with Vitest and Testing Library and never starts Go. The data comes in through `./route.gen`: the page reads it with `page.useLoaderData()` and sends a form with `POST`, both from that file, so the test replaces the file with `vi.mock` and hands the page the data a `Load` would return. Install the packages as dev dependencies:

```sh
pnpm add -D -E vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom
```

`vitest.config.ts` takes the Vite config of the app, so the `tifa/client` import and the aliases work as in the build, and adds the test settings:

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

`vitest.setup.ts` adds the matchers and cleans up after each test:

```ts
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)
```

The test sits next to the page, `app/page.test.tsx`. This one is for the home page of the app `tifa new` writes:

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

Run it with `pnpm exec vitest run`.

- `PageData` is the type of what the Go `Load` returns, so a fixture that no longer fits the Go struct fails `tsc` and `tifa check` (they check test files too). Run `go tool tifa gen` after a change to a Go type, as for any page.
- A page that uses `page.useParams()`, `page.useSearch()` or `layout.useLoaderData()` needs those in the mock too: add the function to the object `./route.gen` is replaced with.
- The mock of `@tanstack/react-router` keeps the real module and replaces `Link` and `useRouter`, which need a router around them. `tifa/client` loads the router module as well, so a mock without `importOriginal` fails with `No "createSerializationAdapter" export`.
- A failed `POST` arrives in the page as a `TifaError` with `status`, `message` and `fields`, the same as in the browser. Build one in the test to try the way the page shows the errors of `tifa.Fields`.
- The test checks the component, not Go: that `Validate` returns that message is for a Go test. How the page and the server fit together is for [a page in a browser](#a-page-in-a-browser).

## A page in a browser

A test with Playwright opens the page in a real browser, so it covers the hydration, the click, the call to the Go handler and the answer on screen. The Go test starts the app with `tifatest.StartPages`, which serves the pages with the front end of `.tifa/dist`, and passes its address to Playwright. Install Playwright once, and build the front end, which `StartPages` reads:

```sh
pnpm add -D -E @playwright/test @types/node
pnpm exec playwright install chromium
go tool tifa build
```

`playwright.config.ts` takes the address from the environment. The server of `tifatest` uses HTTPS with a certificate of its own, so the browser must not check it:

```ts
import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  use: { baseURL: process.env.BASE_URL, ignoreHTTPSErrors: true },
})
```

`e2e/todos.spec.ts` is what a visitor does:

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

The Go side is a test of its own, `app/browser_test.go`. It points the database at an empty file, starts the app and runs Playwright with the address:

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

Run it with `E2E=1 go test ./app -run TestInBrowser`. Playwright prints what failed in the test output. It also writes `test-results/` and `playwright-report/`, which belong in `.gitignore`.

- The test sets `E2E=1` as a switch, so `go test ./...` and `tifa check` do not need a browser. Take the switch out to run it everywhere.
- `StartPages` fails the test when a page, `package.json` or another file the front end was built from changed since `tifa build`. Build again before the run, or keep `tifa dev` running. See [Testing a whole request](testing-apps.md#testing-a-whole-request).
- One app runs per test binary, so keep the browser tests in one Go test, with the specs in one or several files, and do not call `t.Parallel`. The database is the empty file of the test, so each run starts from nothing. A spec that needs rows adds them through the page, as the example does.
- The server listens on this machine only, for as long as the Go test runs. Playwright cannot start without it, so run the specs through the Go test and not with `pnpm exec playwright test` alone.
