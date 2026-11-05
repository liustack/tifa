package ssr

import "github.com/liustack/quickjs"

// React DOM writes the start tag of most elements with pushStartGenericElement and the functions it
// calls for each attribute and style, and the end tag with endChunkForTag. The tifa plugin makes the
// module of its string renderer call __tifa_start_tag and __tifa_end_tag in their place when a VM
// has them, see js/vite/plugin.js. They are those functions written in C in the engine. The start
// tag gives React's text in one string, and hands back an object of the caller's, having changed
// nothing, for any element where it cannot be sure to, which React's own function then writes.

// registerTags defines __tifa_start_tag and __tifa_end_tag on vm.
func registerTags(vm *quickjs.VM) error {
	if err := vm.RegisterStartTag("__tifa_start_tag"); err != nil {
		return err
	}
	return vm.RegisterEndTag("__tifa_end_tag")
}

// emitCheck, when set, defines on each VM what a bundle built with TIFA_EMIT_CHECK=1 reports its
// comparisons of the tags to. Only a build with the tag tifa_emit_check sets it, see emitcheck.go.
var emitCheck func(vm *quickjs.VM) error
