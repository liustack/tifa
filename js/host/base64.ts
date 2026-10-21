import { DOMException } from './dom-exception'
import { getter, uncurry } from './intrinsics'

const { Uint8Array, TypeError } = globalThis
const length = getter(Object.getPrototypeOf(Uint8Array.prototype), 'length')
const fromBase64 = Uint8Array.fromBase64
const toBase64 = uncurry(Uint8Array.prototype.toBase64)
const charCodeAt = uncurry(String.prototype.charCodeAt)
const fromCharCode = String.fromCharCode

export function atob(data: string) {
  if (!arguments.length) throw new TypeError('atob requires an argument')
  // HTML forgiving-base64 decode removes only ASCII whitespace. The engine's
  // loose decoder accepts missing padding and nonzero trailing bits.
  const text = `${data}`
  let input = ''
  for (let i = 0; i < text.length; i++) {
    const c = charCodeAt(text, i)
    if (c !== 9 && c !== 10 && c !== 12 && c !== 13 && c !== 32) input += text[i]
  }
  let bytes: Uint8Array
  try {
    bytes = fromBase64(input)
  } catch {
    throw new DOMException('Invalid base64 string', 'InvalidCharacterError')
  }
  let result = ''
  for (let i = 0; i < length(bytes); i++) result += fromCharCode(bytes[i])
  return result
}

export function btoa(data: string) {
  if (!arguments.length) throw new TypeError('btoa requires an argument')
  const input = `${data}`
  const bytes = new Uint8Array(input.length)
  for (let i = 0; i < input.length; i++) {
    const code = charCodeAt(input, i)
    if (code > 255) throw new DOMException('Character is outside the byte range', 'InvalidCharacterError')
    bytes[i] = code
  }
  return toBase64(bytes)
}
