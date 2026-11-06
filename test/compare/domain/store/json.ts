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

/** Shape names the members of an object that are kept and, for an object in one, its own shape. */
export interface Shape {
  [member: string]: unknown
}

/** stats counts what the reader builds, for the test that unknown members are not built. */
export const stats = { containers: 0 }

/** A number as written. */
export class Num {
  readonly text: string
  constructor(text: string) {
    this.text = text
  }
}

export type Value = string | boolean | null | Num | Value[] | Obj
export type Obj = Map<string, Value>

// maxDepth is the most levels of arrays and objects Go reads (encoding/json/jsontext).
const maxDepth = 10000
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
export function parseBody(bytes: Uint8Array, shape?: Shape): Value {
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
  } catch {
    throw new BadJSON()
  }
  return parse(text, shape)
}

export function parse(text: string, rootShape?: Shape): Value {
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
  // The value is read with a stack of its own, not the stack of the language: Go reads up to
  // maxDepth levels of arrays and objects, and a call for each level would not get that far.
  //
  // A member that the shape does not name is read and checked (its names are not given twice, its
  // depth is counted) but not kept, as Go reads the value of a member it has no field for and drops it.
  interface Frame {
    kind: '{' | '['
    obj?: Obj
    /** seen are the names of an object, kept or not, to refuse one given twice. */
    seen?: Set<string>
    list?: Value[]
    key?: string
    skip: boolean
    shape: Shape | undefined
    /** nextSkip and nextShape are for the value that comes next in this container. */
    nextSkip: boolean
    nextShape: Shape | undefined
  }
  const stack: Frame[] = []
  let result: Value = null
  const finish = (v: Value) => {
    const top = stack[stack.length - 1]
    if (!top) result = v
    else if (top.skip || top.nextSkip) return
    else if (top.obj) top.obj.set(top.key as string, v)
    else (top.list as Value[]).push(v)
  }
  // key reads the name of a member and its colon, and decides whether its value is kept.
  const key = (frame: Frame): void => {
    space()
    if (text[i] !== '"') return fail()
    const name = string()
    const seen = frame.seen as Set<string>
    if (seen.has(name)) return fail()
    seen.add(name)
    space()
    if (text[i++] !== ':') return fail()
    frame.key = name
    const known = frame.shape === undefined || Object.hasOwn(frame.shape, name)
    frame.nextSkip = frame.skip || !known
    const sub = frame.shape?.[name]
    frame.nextShape = sub !== null && typeof sub === 'object' ? (sub as Shape) : undefined
  }
  const open = (kind: '{' | '['): Frame => {
    const parent = stack[stack.length - 1]
    const skip = parent ? parent.nextSkip : false
    const shape = parent ? parent.nextShape : rootShape
    const frame: Frame = { kind, skip, shape, nextSkip: skip, nextShape: undefined }
    if (kind === '{') {
      frame.seen = new Set()
      if (!skip) frame.obj = new Map()
    } else if (!skip) frame.list = []
    if (!skip) stats.containers++
    return frame
  }
  let expectValue = true
  outer: for (;;) {
    if (expectValue) {
      space()
      const c = text[i]
      if (c === '{' || c === '[') {
        // Go counts a container when it opens, an empty one too.
        if (stack.length + 1 > maxDepth) fail()
        i++
        space()
        const frame = open(c)
        if (c === '{' && text[i] === '}') {
          i++
          finish(frame.obj ?? null)
        } else if (c === '[' && text[i] === ']') {
          i++
          finish(frame.list ?? null)
        } else {
          stack.push(frame)
          if (c === '{') key(frame)
          else frame.nextSkip = frame.skip
          continue
        }
      } else if (c === '"') finish(string())
      else if (c === 't') finish(literal('true', true))
      else if (c === 'f') finish(literal('false', false))
      else if (c === 'n') finish(literal('null', null))
      else finish(number())
    }
    expectValue = false
    for (;;) {
      if (stack.length === 0) break outer
      space()
      const top = stack[stack.length - 1]
      const c = text[i++]
      if (top.kind === '[') {
        if (c === ']') {
          stack.pop()
          finish(top.list ?? null)
          continue
        }
        if (c !== ',') return fail()
      } else {
        if (c === '}') {
          stack.pop()
          finish(top.obj ?? null)
          continue
        }
        if (c !== ',') return fail()
        key(top)
      }
      expectValue = true
      break
    }
  }
  space()
  if (i !== text.length) fail()
  return result
}
