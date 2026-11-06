// A reader for JSON bodies with the rules of Go's encoding/json/v2, which tifa binds a request
// body with (internal/core/bind.go): JSON.parse is looser. This one
//
//   - refuses a name given twice in an object, at any depth, unknown names included;
//   - keeps a number as the text it was written in, so that an integer field can refuse 1.0, 1e0
//     and a number outside int64, as Go does for an int;
//   - refuses text after the value, bytes that are not UTF-8 and an unpaired surrogate in an escape.
//
// A problem of any kind throws BadJSON.

export class BadJSON extends Error {}

/** A number as written. */
export class Num {
  readonly text: string
  constructor(text: string) {
    this.text = text
  }
}

export type Value = string | boolean | null | Num | Value[] | Obj
export type Obj = Map<string, Value>

const int64Min = -(1n << 63n)
const int64Max = (1n << 63n) - 1n

/** asInt is the value of a number written as an integer that fits int64, or undefined. */
export function asInt(n: Num): number | undefined {
  if (!/^-?(0|[1-9][0-9]*)$/.test(n.text)) return undefined
  const big = BigInt(n.text)
  if (big < int64Min || big > int64Max) return undefined
  return Number(big)
}

/** parseBody reads the bytes of a body as one JSON value. */
export function parseBody(bytes: Uint8Array): Value {
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
  } catch {
    throw new BadJSON()
  }
  return parse(text)
}

export function parse(text: string): Value {
  let i = 0
  const fail = (): never => {
    throw new BadJSON()
  }
  const space = () => {
    while (i < text.length && ' \t\n\r'.includes(text[i])) i++
  }
  const literal = (word: string, value: Value): Value => {
    if (!text.startsWith(word, i)) return fail()
    i += word.length
    return value
  }
  const string = (): string => {
    i++ // the opening quote
    let out = ''
    for (;;) {
      if (i >= text.length) return fail()
      const c = text[i++]
      if (c === '"') return out
      if (c < ' ') return fail()
      if (c !== '\\') {
        out += c
        continue
      }
      const e = text[i++]
      switch (e) {
        case '"': case '\\': case '/': out += e; break
        case 'b': out += '\b'; break
        case 'f': out += '\f'; break
        case 'n': out += '\n'; break
        case 'r': out += '\r'; break
        case 't': out += '\t'; break
        case 'u': {
          const unit = (): number => {
            const hex = text.slice(i, i + 4)
            if (!/^[0-9a-fA-F]{4}$/.test(hex)) return fail()
            i += 4
            return parseInt(hex, 16)
          }
          const hi = unit()
          if (hi >= 0xdc00 && hi <= 0xdfff) return fail()
          if (hi >= 0xd800 && hi <= 0xdbff) {
            if (text[i] !== '\\' || text[i + 1] !== 'u') return fail()
            i += 2
            const lo = unit()
            if (lo < 0xdc00 || lo > 0xdfff) return fail()
            out += String.fromCharCode(hi, lo)
          } else {
            out += String.fromCharCode(hi)
          }
          break
        }
        default:
          return fail()
      }
    }
  }
  const number = (): Num => {
    const m = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?/.exec(text.slice(i))
    if (!m) return fail()
    i += m[0].length
    return new Num(m[0])
  }
  const value = (depth: number): Value => {
    if (depth > 10000) return fail()
    space()
    const c = text[i]
    if (c === '{') {
      i++
      const obj: Obj = new Map()
      space()
      if (text[i] === '}') {
        i++
        return obj
      }
      for (;;) {
        space()
        if (text[i] !== '"') return fail()
        const name = string()
        if (obj.has(name)) return fail()
        space()
        if (text[i++] !== ':') return fail()
        obj.set(name, value(depth + 1))
        space()
        const next = text[i++]
        if (next === '}') return obj
        if (next !== ',') return fail()
      }
    }
    if (c === '[') {
      i++
      const list: Value[] = []
      space()
      if (text[i] === ']') {
        i++
        return list
      }
      for (;;) {
        list.push(value(depth + 1))
        space()
        const next = text[i++]
        if (next === ']') return list
        if (next !== ',') return fail()
      }
    }
    if (c === '"') return string()
    if (c === 't') return literal('true', true)
    if (c === 'f') return literal('false', false)
    if (c === 'n') return literal('null', null)
    return number()
  }
  const result = value(0)
  space()
  if (i !== text.length) fail()
  return result
}
