// The codec of the query parameters of a page. The Go struct of a Load or Layout says the type of
// each parameter, tifa build writes that into the route as a spec, and the router converts what it
// reads from the address by the spec and by nothing else. The rules are those of decodeQuery in
// internal/core/query.go, and one table of cases (internal/core/testdata/query-values.txt) tests
// both, in Go, in Node and in beastjs.
//
// A string is any text. A bool is true or false. A whole number is digits with an optional sign and an
// optional point with zeros after it (+007, 1.0, -0), and fits the width of its type and 2^53-1. A float
// is digits with an optional sign, fraction and exponent (010.50, .5, 5., 1e3), and is 0 or between 1e-6
// and 1e21 in size. A number written another way than the shortest one is the same number: the page is
// rendered for the shortest text (canonicalNumber) and the router writes the address that way. No
// spaces, no hex, no NaN, and 10.50 or 1e3 is no whole number.

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

// maxNumberText is the longest text of a number, in bytes, which is its length in characters since only
// ASCII passes the test below. A very long decimal is rounded differently by Go and by a browser, so no
// number a person types or a form sends is that long. It is maxNumberText in internal/core/query.go.
const maxNumberText = 64

const wholeText = /^[+-]?[0-9]+(\.0*)?$/
const floatText = /^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$/

// readNumber reads the number text writes, as readNumber of internal/core/query.go does: whole is true
// for a whole number and false for a float. The number is 0 or between 1e-6 and 1e21 in size, a whole
// number is at most 2^53-1 in size, and -0 is 0. It is undefined for a text that is no such number.
function readNumber(text: string, whole: boolean): number | undefined {
  if (text.length > maxNumberText || !(whole ? wholeText : floatText).test(text)) return undefined
  const n = Number(text)
  const size = Math.abs(n)
  if (size !== 0 && (size < 1e-6 || size >= 1e21)) return undefined
  if (whole && size > maxExact) return undefined
  // A number too small to hold reads as 0, which is not what the text says.
  if (n === 0 && /[1-9]/.test(text.split(/e/i)[0]!)) return undefined
  return n === 0 ? 0 : n
}

// canonicalNumber is the shortest text of the number text writes, the one the page is rendered for,
// undefined for a text that is no such number. It is CanonicalNumber in internal/core/query.go.
export function canonicalNumber(text: string, whole: boolean): string | undefined {
  const n = readNumber(text, whole)
  return n === undefined ? undefined : String(n)
}

// decodeValue reads one value of a kind from its text. It returns undefined for a text that is not
// valid, which no value is: a string, number or bool comes back otherwise.
export function decodeValue(kind: string, text: string): string | number | boolean | undefined {
  switch (kind) {
    case 'string':
      return text
    case 'bool':
      return text === 'true' ? true : text === 'false' ? false : undefined
    case 'float64':
      return readNumber(text, false)
  }
  const range = ranges[kind]
  if (range === undefined) throw new Error(`the query codec has no kind ${kind}`)
  const n = readNumber(text, true)
  return n !== undefined && n >= range[0] && n <= range[1] ? n : undefined
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
// the codec left out stays out. A parameter that is undefined stays undefined, which the router writes as none and a Link that asks for it with explicitUndefined sees. A Layout and a page that name the same parameter both convert it, and
// a text stays a text.
export function searchText<S>(spec: Spec): (context: { search: S; next: (search: S) => S }) => S {
  const fields = new Map(parseSpec(spec).map(({ name, list }) => [name, list]))
  const lists = new Set([...fields].filter(([, list]) => list).map(([name]) => name))
  const text = (value: unknown, key: string) => (fields.has(key) && (typeof value === 'number' || typeof value === 'boolean') ? String(value) : value)
  return (({ search, next }: { search: Record<string, unknown>; next: (search: unknown) => Record<string, unknown> }) => {
    const result = next(search)
    const out: Record<string, unknown> = Object.create(null)
    for (const key of Object.keys(result)) {
      const value = result[key]
      if (!lists.has(key) || !Array.isArray(value)) {
        out[key] = text(value, key)
        continue
      }
      // A list is written as parseSearch reads the address back: one element is the single text, two
      // or more are the array, and an empty list is no key.
      if (value.length === 1) out[key] = text(value[0], key)
      else if (value.length > 1) out[key] = value.map((item) => text(item, key))
    }
    return out
  }) as never
}
