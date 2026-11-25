// The codec of the query parameters of a page. The Go struct of a Load or Layout says the type of
// each parameter, tifa build writes that into the route as a spec, and the router converts what it
// reads from the address by the spec and by nothing else. The rules are those of decodeQuery in
// internal/core/query.go, and one table of cases (internal/core/testdata/query-values.txt) tests
// both, in Go, in Node and in beastjs.
//
// A value is valid only when it is the one way to write it: decoding it and writing the result gives
// the same text. A string is any text. A whole number is 0, or digits with no leading zero and an
// optional minus, and fits the width of its type and 2^53-1. A bool is true or false. A float has no
// exponent, no trailing zero after the point and at most 15 significant digits, and is 0 or between
// 1e-6 and 1e21 in size.

// InvalidQuery is what the codec throws for a value that is not valid. The router wraps what a
// validateSearch throws in a SearchParamError, and the router of Tifa makes a TifaError with status
// 400 and this message of it (searchFailed in router.tsx), as Go answers for the same address. It is
// an error of its own so that the codec needs no other module of the runtime.
export class InvalidQuery extends Error {
  readonly status = 400
  constructor(name: string) {
    super(`invalid query parameter ${JSON.stringify(name)}`)
    this.name = 'InvalidQuery'
  }
}

// Spec lists the name of each parameter with its kind: string, bool, int, int8, int16, int32, int64,
// uint, uint8, uint16, uint32, uint64, uintptr or float64, with [] after it for a list. It is a list of
// pairs and not an object, since a parameter may be called __proto__, which an object literal would
// take for its prototype.
export type Spec = ReadonlyArray<readonly [string, string]>

// maxExact is the largest whole number a browser holds exactly, 2^53-1.
const maxExact = Number.MAX_SAFE_INTEGER

const ranges: Record<string, [number, number]> = {
  int: [-maxExact, maxExact],
  int8: [-128, 127],
  int16: [-32768, 32767],
  int32: [-2147483648, 2147483647],
  int64: [-maxExact, maxExact],
  uint: [0, maxExact],
  uint8: [0, 255],
  uint16: [0, 65535],
  uint32: [0, 4294967295],
  uint64: [0, maxExact],
  uintptr: [0, maxExact],
}

const wholeNumber = /^-?(0|[1-9][0-9]*)$/
const plainFloat = /^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$/

// decodeValue reads one value of a kind from its text. It returns undefined for a text that is not
// valid, which no value is: a string, number or bool comes back otherwise.
export function decodeValue(kind: string, text: string): string | number | boolean | undefined {
  switch (kind) {
    case 'string':
      return text
    case 'bool':
      return text === 'true' ? true : text === 'false' ? false : undefined
    case 'float64': {
      if (!plainFloat.test(text) || text === '-0') return undefined
      // The digits after the leading zeros of the number, the point not counted.
      if (text.replace('-', '').replace('.', '').replace(/^0+/, '').length > 15) return undefined
      const n = Number(text)
      const size = Math.abs(n)
      if (size !== 0 && (size < 1e-6 || size >= 1e21)) return undefined
      // What the number holds must write back as the text: digits too small to hold read as 0.
      return String(n) === text ? n : undefined
    }
  }
  const range = ranges[kind]
  if (range === undefined) throw new Error(`the query codec has no kind ${kind}`)
  if (!wholeNumber.test(text) || text === '-0' || text.replace('-', '').length > 16) return undefined
  const n = Number(text)
  return n >= range[0] && n <= range[1] ? n : undefined
}

const invalid = Symbol('invalid')

// decodeOne reads one value that is a text from the address or a value a Link or navigate gave,
// which is already of the type of the field: such a number is read as the text it is written as.
// undefined is a value that was not given, and so is the empty text of a field that is not a string.
function decodeOne(kind: string, value: unknown): unknown {
  if (value === undefined) return undefined
  if (kind === 'string') return typeof value === 'string' ? value : invalid
  if (typeof value === 'boolean') return kind === 'bool' ? value : invalid
  if (typeof value === 'number') value = String(value)
  if (typeof value !== 'string') return invalid
  if (value === '') return undefined
  return decodeValue(kind, value) ?? invalid
}

function decodeField(kind: string, list: boolean, value: unknown): unknown {
  if (!list) return Array.isArray(value) ? invalid : decodeOne(kind, value)
  const items: unknown[] = []
  for (const item of Array.isArray(value) ? value : [value]) {
    const one = decodeOne(kind, item)
    if (one === invalid) return invalid
    if (one !== undefined) items.push(one)
  }
  // A list with nothing in it is a parameter that was not passed.
  return items.length > 0 ? items : undefined
}

// searchCodec makes the validateSearch of a route from the kinds of the parameters its Go function
// reads. It takes the strings and arrays of strings that parseSearch reads from the address, and the
// typed values of a Link or navigate, and gives the typed search. Parameters the spec does not name
// stay as they are. A value that is not valid, or a name that comes twice and is not a list, throws
// an InvalidQuery with the message that Go answers with for the same address.
//
// The router lays the result over the parsed search, so a parameter that was not given, an empty
// value of a number or bool among them, is returned as undefined and not left out. A route below
// another gets the converted values of the route above, so a value that is already typed is valid.
export function searchCodec<S>(spec: Spec): (search: S) => S {
  const fields = parseSpec(spec)
  return ((raw: Record<string, unknown>) => {
    // The first parameter that is not valid in the order of the names is the one the error names, as
    // in Go, where a page with two of them gets one answer.
    const decoded = new Map<string, unknown>()
    for (const { name, kind, list } of fields) {
      if (!Object.hasOwn(raw, name)) continue
      const value = decodeField(kind, list, raw[name])
      if (value === invalid) throw new InvalidQuery(name)
      decoded.set(name, value)
    }
    // The parameters keep the order of the address, so the router writes the same address back.
    const out: Record<string, unknown> = Object.create(null)
    for (const key of Object.keys(raw)) out[key] = decoded.has(key) ? decoded.get(key) : raw[key]
    return out
  }) as never
}

function parseSpec(spec: Spec) {
  return spec
    .map(([name, kind]) => {
      const list = kind.endsWith('[]')
      return { name, kind: list ? kind.slice(0, -2) : kind, list }
    })
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
}

// searchText makes the search middleware of a route that has a codec. A Link or navigate gives numbers
// and booleans, and the router compares the search of a Link with the search of the location, which it
// read from the address as text, to tell whether the Link is the page it is on. So the middleware
// gives the numbers and booleans of the parameters of the spec back as the text the router writes
// for them (String), and the Link is active at the address it makes. It sits outside the validation
// that the router adds when it builds the address of a page (_includeValidateSearch), so a parameter
// the codec left out stays out (and an undefined one is left out, as the router writes none). A Layout and a page that name the same parameter both convert it, and
// a text stays a text.
export function searchText<S>(spec: Spec): (context: { search: S; next: (search: S) => S }) => S {
  const lists = new Map(parseSpec(spec).map(({ name, list }) => [name, list]))
  const text = (value: unknown) => (typeof value === 'number' || typeof value === 'boolean' ? String(value) : value)
  return (({ search, next }: { search: Record<string, unknown>; next: (search: unknown) => Record<string, unknown> }) => {
    const result = next(search)
    const out: Record<string, unknown> = Object.create(null)
    for (const key of Object.keys(result)) {
      const value = result[key]
      if (value === undefined) continue
      out[key] = lists.has(key) ? (Array.isArray(value) ? value.map(text) : text(value)) : value
    }
    return out
  }) as never
}
