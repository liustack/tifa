package ssr

import "github.com/liustack/slowjs"

// The async context of the VM is the object promise jobs carry (see slowjs.GetAsyncContext):
// the AsyncLocalStorage of js/host keeps its stores in it. The host takes the two functions from
// __tifa_async_context__ and removes it before any application code runs.
const asyncContextHook = `(get, set) => {
  Object.defineProperty(globalThis, '__tifa_async_context__', { value: { get, set }, configurable: true })
}`

func registerAsyncContext(vm *slowjs.VM) error {
	define, err := vm.EvalValue(asyncContextHook, slowjs.EvalGlobal)
	if err != nil {
		return err
	}
	defer define.Free()
	get, err := vm.NewBuiltinFunction(slowjs.GetAsyncContext)
	if err != nil {
		return err
	}
	defer get.Free()
	set, err := vm.NewBuiltinFunction(slowjs.SetAsyncContext)
	if err != nil {
		return err
	}
	defer set.Free()
	r, err := define.CallValue(slowjs.UndefinedValue, get, set)
	if err != nil {
		return err
	}
	r.Free()
	return nil
}
