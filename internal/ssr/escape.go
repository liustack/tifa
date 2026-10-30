package ssr

import "github.com/liustack/slowjs"

// React DOM escapes every text and attribute value it writes with escapeTextForBrowser. The tifa
// plugin makes the module of its string renderer use __tifa_escape in its place when a VM has it,
// see js/vite/plugin.js. It is that function written in C in the engine: it gives the text React's
// function gives for any argument, and costs one call.

// registerEscape defines __tifa_escape on vm.
func registerEscape(vm *slowjs.VM) error {
	return vm.RegisterEscapeText("__tifa_escape")
}
