// Capture before application modules run. Calling a saved method must not look
// up Function.prototype.call or Reflect.apply again after a VM is reused.
const apply = Reflect.apply
const descriptor = Object.getOwnPropertyDescriptor
export const uncurry = (fn: Function) => (receiver: unknown, ...args: any[]) => apply(fn, receiver, args)
export const getter = (prototype: object, key: PropertyKey) => uncurry(descriptor(prototype, key)!.get!)
