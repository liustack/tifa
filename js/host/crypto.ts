import { DOMException } from './dom-exception'
import { getter, uncurry } from './intrinsics'

const { Uint8Array, TypeError } = globalThis
const isView = ArrayBuffer.isView
const fromBase64 = Uint8Array.fromBase64
const set = uncurry(Uint8Array.prototype.set)
const toHex = uncurry(Uint8Array.prototype.toHex)
const slice = uncurry(String.prototype.slice)
const exec = uncurry(RegExp.prototype.exec)
const integer = /^(?:Uint8Clamped|(?:Int|Uint|BigInt|BigUint)(?:8|16|32|64))Array$/

// Installed by ssr.Load before any bundle module runs. Capture it in this module
// and remove the private global before application code runs.
const randomBytes = globalThis.__tifa_random_bytes__
Reflect.deleteProperty(globalThis, '__tifa_random_bytes__')
const typedArray = Object.getPrototypeOf(Uint8Array.prototype)
const kind = getter(typedArray, Symbol.toStringTag)
const buffer = getter(typedArray, 'buffer')
const byteOffset = getter(typedArray, 'byteOffset')
const byteLength = getter(typedArray, 'byteLength')
const dataViewBuffer = getter(DataView.prototype, 'buffer')
const resizable = getter(ArrayBuffer.prototype, 'resizable')

function bytes(length: number): Uint8Array {
  return fromBase64(randomBytes(length))
}

export const crypto = {
  getRandomValues<T extends ArrayBufferView>(array: T): T {
    if (this !== crypto) throw new TypeError('Illegal invocation')
    if (!isView(array)) throw new TypeError('Expected an ArrayBufferView')
    const name = kind(array)
    const backing = name === undefined ? dataViewBuffer(array) : buffer(array)
    // This getter also rejects SharedArrayBuffer. Web IDL converts the view
    // before crypto checks its element type, quota or zero length.
    if (resizable(backing)) throw new TypeError('Resizable ArrayBuffer is not allowed')
    if (!exec(integer, name)) {
      throw new DOMException('Expected an integer typed array', 'TypeMismatchError')
    }
    const size = byteLength(array)
    if (size > 65536) throw new DOMException('Requested more than 65536 random bytes', 'QuotaExceededError')
    if (size === 0) return array
    set(new Uint8Array(backing, byteOffset(array), size), bytes(size))
    return array
  },
  randomUUID(): string {
    if (this !== crypto) throw new TypeError('Illegal invocation')
    const data = bytes(16)
    data[6] = (data[6] & 15) | 64
    data[8] = (data[8] & 63) | 128
    const hex = toHex(data)
    return `${slice(hex,0,8)}-${slice(hex,8,12)}-${slice(hex,12,16)}-${slice(hex,16,20)}-${slice(hex,20)}`
  },
}
