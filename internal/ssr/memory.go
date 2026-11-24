package ssr

import "github.com/liustack/beastjs"

// VMMemory returns what the engine counts in vm, after a full GC when collect is set. Like Compile,
// Load and RenderVM it is for measurements: it walks every object of the VM.
func VMMemory(vm *beastjs.VM, collect bool) beastjs.MemoryUsage {
	if collect {
		vm.RunGC()
	}
	return vm.MemoryUsage()
}
