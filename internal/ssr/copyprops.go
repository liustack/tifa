package ssr

import "github.com/liustack/slowjs"

// React's createElement and cloneElement copy the keys of their config into the props of the
// element with a loop that takes about 250 ns for each key. The tifa plugin makes them ask
// __tifa_copy_props first when a VM has it, see js/vite/plugin.js. It is that loop written in C in
// the engine: it copies where the loop and it cannot differ in anything a page can see, and answers
// false, having changed nothing, for any other config, which then goes through the loop.

// registerCopyProps defines __tifa_copy_props on vm.
func registerCopyProps(vm *slowjs.VM) error {
	return vm.RegisterCopyProps("__tifa_copy_props")
}
