package bundle

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/ssr"
)

// alsStore uses AsyncLocalStorage and AsyncResource of node:async_hooks the ways a package does
// that keeps the state of a request in one. Node gives what the test wants for the same code.
const alsStore = "import { AsyncLocalStorage, AsyncResource } from 'node:async_hooks'\n" +
	"const als = new AsyncLocalStorage()\n" +
	"const other = new AsyncLocalStorage()\n" +
	"export { AsyncLocalStorage }\n" +
	"export const later = () => import('./later.js').then((m) => m.storage.run('later', async () => { await null; return m.storage.getStore() }))\n" +
	"export async function render(url) {\n" +
	"  const log = []\n" +
	"  await als.run({ url }, async () => {\n" +
	"    await null\n" +
	"    log.push(als.getStore().url)\n" +
	"    await other.run('o', async () => { await Promise.resolve(); log.push(als.getStore().url + '+' + other.getStore()) })\n" +
	"    log.push(String(other.getStore()))\n" +
	"    log.push(String(als.exit(() => als.getStore())))\n" +
	"    const snap = AsyncLocalStorage.snapshot()\n" +
	"    const bound = AsyncLocalStorage.bind((x) => als.getStore().url + x)\n" +
	"    const res = new AsyncResource('x')\n" +
	"    const resBound = res.bind(function () { return als.getStore().url + this.n })\n" +
	"    await als.run({ url: 'inner' }, async () => {\n" +
	"      log.push(snap(() => als.getStore().url))\n" +
	"      log.push(bound('!'))\n" +
	"      log.push(res.runInAsyncScope(function (y) { return als.getStore().url + this.n + y }, { n: 1 }, 2))\n" +
	"      log.push(resBound.call({ n: 3 }))\n" +
	"      log.push(AsyncResource.bind(() => als.getStore().url)())\n" +
	"      log.push(als.run({ url: 'sync' }, (a, b) => als.getStore().url + a + b, 4, 5))\n" +
	"    })\n" +
	"  })\n" +
	"  log.push(String(als.getStore()))\n" +
	"  return log.join(' ')\n" +
	"}\n"

// The server has AsyncLocalStorage, as a global and as an export of node:async_hooks: its store
// follows await and promise reactions, also when two runs overlap, and a package that imports it,
// from a module the bundle loads or one only an import() reaches, gets no warning: nothing of it
// throws.
func TestSSRAsyncLocalStorageFollowsAwait(t *testing.T) {
	pool, output, err := nodePool(t, map[string]string{
		"vendor/node_modules/als-store/package.json": `{"name":"als-store","type":"module","main":"index.js"}`,
		"vendor/node_modules/als-store/index.js":     alsStore,
		// A module only an import() reaches runs only then.
		"vendor/node_modules/als-store/later.js": "import { AsyncLocalStorage } from 'node:async_hooks'\nexport const storage = new AsyncLocalStorage()\n",
		".tifa/server.ts": "import './runtime/host.js'\nimport { AsyncLocalStorage, render, later } from '../vendor/node_modules/als-store/index.js'\n" +
			"(globalThis as any).startRender = () => {\n" +
			"  Promise.all([render('/a'), render('/b'), later()]).then((r) => {\n" +
			"    (globalThis as any).renderResult = '<p>' + (globalThis.AsyncLocalStorage === AsyncLocalStorage) + '|' + r.join('|') + '</p>'\n" +
			"  })\n}\n",
	})
	if err != nil {
		t.Fatalf("prewarm: %v", err)
	}
	const want = "<p>true|/a /a+o undefined undefined /a /a! /a12 /a3 inner sync45 undefined|/b /b+o undefined undefined /b /b! /b12 /b3 inner sync45 undefined|later</p>"
	for range 2 {
		if html, err := pool.Render(t.Context(), ssr.Input{URL: "/"}); err != nil || html != want {
			t.Errorf("render = %q, %v, want %q", html, err, want)
		}
	}
	if strings.Contains(output, "async_hooks") {
		t.Errorf("the build prints:\n%s", output)
	}
}

// The other exports of node:async_hooks are stand-ins that throw when used, as those of any Node
// module, and the build names the module.
func TestSSRAsyncHooksKeepsItsStandIns(t *testing.T) {
	pool, output, err := nodePool(t, map[string]string{
		"vendor/node_modules/hooks-user/package.json": `{"name":"hooks-user","type":"module","main":"index.js"}`,
		"vendor/node_modules/hooks-user/index.js":     "import { createHook } from 'node:async_hooks'\nexport const hook = () => createHook({})\n",
		".tifa/server.ts": "import './runtime/host.js'\nimport { hook } from '../vendor/node_modules/hooks-user/index.js'\n" +
			"(globalThis as any).startRender = () => {\n  try {\n    hook()\n  } catch (e: any) {\n    (globalThis as any).renderResult = '<p>' + e.message + '</p>'\n  }\n}\n",
	})
	if err != nil {
		t.Fatalf("prewarm: %v", err)
	}
	html, err := pool.Render(t.Context(), ssr.Input{URL: "/"})
	if err != nil || !strings.Contains(html, `createHook of the Node module "node:async_hooks", imported by hooks-user, was used on the server`) {
		t.Errorf("render = %q, %v", html, err)
	}
	if !strings.Contains(output, `"node:async_hooks", imported by hooks-user`) {
		t.Errorf("the build prints:\n%s", output)
	}
}
