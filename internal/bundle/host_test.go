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
