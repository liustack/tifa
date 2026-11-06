// The query of a URL the way Tifa reads and writes it, so the same URL gives the same page here
// and the links and navigations the storefront makes spell their queries the same. TanStack
// Router's own reader turns "3" into a number and writes arrays as JSON. These are copies of
// parseSearch and stringifySearch of the router Tifa generates (.tifa/runtime/router.tsx).

// parseSearch reads every value as a string, and a key given more than once as the array of its
// values. The object has no prototype, so a key such as __proto__ is a parameter like any other.
export function parseSearch(searchStr: string): Record<string, string | string[]> {
  const search: Record<string, string | string[]> = Object.create(null)
  for (const [key, value] of new URLSearchParams(searchStr)) {
    const previous = search[key]
    if (previous === undefined) search[key] = value
    else if (Array.isArray(previous)) previous.push(value)
    else search[key] = [previous, value]
  }
  return search
}

// stringifySearch writes a query parseSearch reads back as the same values, spelled the way
// browsers and forms send it: a space is +, and only the characters that change the meaning, or
// that browsers escape, are escaped.
export function stringifySearch(search: Record<string, unknown>): string {
  const parts: string[] = []
  for (const key in search) {
    const value = search[key]
    if (value === undefined) continue
    for (const item of Array.isArray(value) ? value : [value]) parts.push(encodeQueryPart(key) + '=' + encodeQueryPart(String(item)))
  }
  return parts.length > 0 ? '?' + parts.join('&') : ''
}

const keptInQuery = /%(24|2C|2F|3A|3F|40|5B|5C|5D|5E|60|7B|7C|7D)/g

function encodeQueryPart(text: string) {
  return encodeURIComponent(text)
    .replace(keptInQuery, (escape) => decodeURIComponent(escape))
    .replace(/'/g, '%27')
    .replace(/%20/g, '+')
}

// pick keeps the query parameters a loader reads, for loaderDeps, in an object without a
// prototype.
export function pick(search: object, keys: string[]): Record<string, string | string[]> {
  const values = search as Record<string, string | string[]>
  const picked: Record<string, string | string[]> = Object.create(null)
  for (const key of keys) if (Object.hasOwn(values, key)) picked[key] = values[key]
  return picked
}
