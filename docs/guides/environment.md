---
summary: 'How a value from the environment, such as a public key or an API base URL, reaches a page of a Tifa app: settings stay in the Go program and the page gets them from Load, why `import.meta.env.VITE_*` and `process.env` are not used for this (as in Vite and Next.js), what import.meta.env still gives, and the build warning for `import.meta.env.VITE_*`'
audience: app
order: 10
read_when:
  - You come from Next.js or Vite and reach for `import.meta.env.VITE_*`, `NEXT_PUBLIC_*` or `process.env` in a page
  - The build warns that `import.meta.env.VITE_*` is fixed when the build runs
  - A page needs a public key, an API base URL or a feature switch that differs between environments
  - Deciding where a secret lives
---

# Values from the environment

In Tifa the environment and the secrets stay in the Go process. The program reads its settings when it starts: flags, environment variables, `.env` and `tifa.yaml` (see [Configuration](../build-and-deploy/config.md)). The front-end build never sees them. Vite does not read `.env` (`envDir: false`), and the build of one app runs in every environment.

To give a value to the browser, return it from `Load`, or from a `Layout` for every page below it. That line of Go is the only way a value reaches a page, so it is always written on purpose and easy to find.

## An example

Define the option with the app settings (see [Options of the app](../build-and-deploy/config.md#options-of-the-app)):

```go
// options.go
var PublicKey = tifa.String("stripe.public_key", "", "Public key the browser uses")
```

Set it where the program runs: `STRIPE_PUBLIC_KEY=pk_live_... ./bin/app`, a line in `.env`, or `--stripe-public-key`. Return it from `Load`:

```go
// app/checkout/route.go
type Page struct {
	PublicKey string `json:"publicKey"`
}

func Load(c *tifa.Ctx) (Page, error) { return Page{PublicKey: shop.PublicKey}, nil }
```

Read it in the page:

```tsx
// app/checkout/page.tsx
import { page } from './route.gen'

export default function Checkout() {
  const { publicKey } = page.useLoaderData()
  return <PayForm publicKey={publicKey} />
}
```

The same build now runs with a test key on staging and a live key in production.

## Why not a name prefix

In Vite and Next.js a name prefix (`VITE_`, `NEXT_PUBLIC_`) is what puts a value into the JavaScript the browser gets, and it happens when the build runs. A new value needs a new build, and a wrong prefix on a secret sends it to every visitor. In Tifa no name does that. A value that is not returned from `Load` is not in the page.

What `Load` returns, every visitor can see. Never return a secret, such as a `tifa.Secret`, a password or a token.

## What `import.meta.env` is still for

Vite still fills `import.meta.env` with these values:

- `MODE`, `DEV` and `PROD`: `production`, `false` and `true` in `tifa build`, in the browser bundle and in the server render. `development`, `true` and `false` in `tifa dev`, in the browser modules and in the server render alike.
- `SSR`: `true` in the server render and `false` in the browser.
- `BASE_URL`: `/` in `tifa build`, and `/__tifa/vite/` in `tifa dev`, where Vite's dev server serves the browser modules, in the server render too.

The server render of `tifa dev` still runs the production build of React and of other libraries: `process.env.NODE_ENV` is `production` there.

Vite also writes into the bundle every variable that starts with `VITE_` in the environment of the shell that runs `tifa build`, and every `import.meta.env.*` constant the app gives to `define` in its `vite.config.ts`. These are fixed when the build runs. They fit values that are the same everywhere the build runs, and not the ones that change with the environment.

`tifa build` and `tifa dev` warn when app code reads `import.meta.env.VITE_*`: once for each name, with the place of the first read. Under `tifa dev` the warning is also in `go tool tifa errors`, under `Warnings`, until a build that no longer has it. The warning covers `import.meta.env.VITE_X`, `import.meta.env['VITE_X']` and `const { VITE_X } = import.meta.env`. It does not cover `import.meta.env` handed on under another name. Code in `node_modules` is not checked.
