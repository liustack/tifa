package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

var hostDir = filepath.Join("..", "..", "js", "host")

func hostDeps(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(hostDir, "node_modules", "core-js")); err != nil {
		t.Skip("js/host has no node_modules, run pnpm install --frozen-lockfile there")
	}
}

// js/runtime/host.js is generated from js/host and checked in, so applications need none of the
// patch packages. Rebuilding it from the pinned sources must give the checked-in bytes.
func TestHostScriptIsUpToDate(t *testing.T) {
	hostDeps(t)
	got, err := Host(hostDir)
	if err != nil {
		t.Fatalf("Host: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("js/runtime/host.js is stale, run go generate ./js")
	}
}

// The checked-in script provides the APIs React and the router need, and no transport.
func TestHostScriptPatchesQuickJS(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	got := evalResult(t, string(script)+`
globalThis.result = [
  new URL('/a?b=1', 'http://x.invalid').searchParams.get('b'),
  new TextDecoder().decode(new TextEncoder().encode('ok')),
  typeof AbortController, typeof Headers, typeof Response, typeof fetch, typeof Request,
].join(',')`)
	if got != "1,ok,function,function,function,undefined,undefined" {
		t.Fatalf("got %s", got)
	}
}

// A render leaves a dead cycle in which the router holds the AbortControllers of its matches and
// their signals hold listeners that lead back to the router. One collection must free all of it,
// or the garbage of each render waits for later collections and the VM grows.
func TestHostScriptFreesDeadAbortSignalsInOneCollection(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	got := evalResult(t, string(script)+`
// collect allocates until a new dead cycle is gone, which takes one collection.
function sentinel() {
  const cycle = {}
  cycle.self = cycle
  return new WeakRef(cycle)
}
function collect() {
  const gone = sentinel()
  const kept = []
  while (gone.deref() !== undefined) kept.push({})
}
function render() {
  const router = { matches: [] }
  router.self = router
  const load = new AbortController()
  router.load = load
  for (let i = 0; i < 4; i++) {
    const controller = new AbortController()
    controller.signal.addEventListener('abort', () => router)
    router.matches.push(controller)
  }
  load.signal.addEventListener('abort', () => router.matches, { once: true })
  return [load, ...router.matches].map((controller) => new WeakRef(controller.signal))
}
const signals = []
for (let i = 0; i < 16; i++) signals.push(...render())
collect()
globalThis.result = signals.filter((signal) => signal.deref() !== undefined).length + ' of ' + signals.length + ' signals left'
`)
	if got != "0 of 80 signals left" {
		t.Fatalf("after one collection: %s", got)
	}
}

// A signal or controller that the application seals, freezes or makes non-extensible still
// aborts and runs its listeners, as it did before the patch that keeps their state off WeakMaps.
func TestHostScriptAbortsFrozenSignals(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	got := evalResult(t, string(script)+`
const results = []
for (const lock of ['seal', 'freeze', 'preventExtensions']) {
  try {
    const controller = new AbortController()
    let heard = 0
    controller.signal.addEventListener('abort', () => heard++)
    controller.signal.onabort = () => heard++
    Object[lock](controller.signal)
    Object[lock](controller)
    controller.abort()
    results.push(lock + ' ' + controller.signal.aborted + ' ' + heard)
  } catch (error) {
    results.push(lock + ' ' + error)
  }
}
globalThis.result = results.join(', ')
`)
	if want := "seal true 2, freeze true 2, preventExtensions true 2"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
