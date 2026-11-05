package ssr

import "github.com/liustack/quickjs"

// The async context of the VM is the object promise jobs carry (see quickjs.GetAsyncContext):
// the AsyncLocalStorage of js/host keeps its stores in it. The host takes the two functions from
// __tifa_async_context__ and removes it before any application code runs.
const asyncContextHook = `(get, set) => {
  Object.defineProperty(globalThis, '__tifa_async_context__', { value: { get, set }, configurable: true })
}`

func registerAsyncContext(vm *quickjs.VM) error {
	define, err := vm.EvalValue(asyncContextHook, quickjs.EvalGlobal)
	if err != nil {
		return err
	}
	defer define.Free()
	get, err := vm.NewBuiltinFunction(quickjs.GetAsyncContext)
	if err != nil {
		return err
	}
	defer get.Free()
	set, err := vm.NewBuiltinFunction(quickjs.SetAsyncContext)
	if err != nil {
		return err
	}
	defer set.Free()
	r, err := define.CallValue(quickjs.UndefinedValue, get, set)
	if err != nil {
		return err
	}
	r.Free()
	return nil
}
