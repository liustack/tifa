package ssr

import (
	"reflect"
	"unsafe"

	"github.com/liustack/quickjs"
	lib "github.com/liustack/quickjs/lib"
	"modernc.org/libc"
)

// VMMemory returns what QuickJS counts in the runtime of vm, after a GC when collect is set. Like
// Compile, Load and RenderVM it is for measurements. Each VM has a runtime of its own. The quickjs
// package does not export the runtime, so VMMemory reads it from the VM, as the fork's own tests do.
func VMMemory(vm *quickjs.VM, collect bool) lib.TJSMemoryUsage {
	rt := (*struct {
		c   uintptr
		tls *libc.TLS
	})(reflect.ValueOf(vm).Elem().FieldByName("runtime").UnsafePointer())
	if collect {
		lib.XJS_RunGC(rt.tls, rt.c)
	}
	n := int(unsafe.Sizeof(lib.TJSMemoryUsage{}))
	p := rt.tls.Alloc(n)
	defer rt.tls.Free(n)
	lib.XJS_ComputeMemoryUsage(rt.tls, rt.c, p)
	// p is C memory of the VM's allocator, which the Go GC neither moves nor frees.
	return *(*lib.TJSMemoryUsage)(unsafe.Add(nil, p))
}
