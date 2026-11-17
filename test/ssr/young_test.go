package ssr_test

import (
	"testing"

	"github.com/liustack/tifa/internal/ssr"
)

// A render of a site with languages makes a router and a prefix rewrite that hold each other. Once
// the render is over both are garbage, and a young collection must free them: nothing that lives
// from the load on may point at them. If one does (a module-level WeakMap from the router to its
// rewrite did), the young collection keeps the pair and only a full GC frees it, so a VM that
// runs young GCs between renders grows by about the size of a router with its page data per render
// and runs full GCs far more often than fullGCEvery.
func TestShopYoungGCFreesTheRouterOfEachRender(t *testing.T) {
	source, pages := shopBundle(t)
	cfg := shopConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	renderRound := func(n int) {
		for i := range n {
			page := pages[i%len(pages)]
			in, input := serverInput(t, page.input)
			html, err := renderVM(vm, in, input, cfg.Timeout)
			if err != nil {
				t.Fatal(err)
			}
			page.check(t, html)
			vm.RunYoungGC()
		}
	}
	// Warm up: the first renders fill caches that stay.
	renderRound(60)
	vm.RunGC()
	liveSize := vm.MemorySize()
	const renders = 240
	renderRound(renders)
	grown := vm.MemorySize() - liveSize
	perRender := grown / renders
	free := ssr.VMMemory(vm.VM, false)
	t.Logf("after a full GC %d KB, after %d renders with young GCs only %d KB (%d B per render), malloc %d KB", liveSize>>10, renders, vm.MemorySize()>>10, perRender, free.MallocSize>>10)
	// Nothing a render makes should stay. A router left behind is tens of KB.
	if perRender > 8<<10 {
		t.Fatalf("the VM grew %d bytes per render under young GCs, the router of each render stays until a full GC", perRender)
	}
}
