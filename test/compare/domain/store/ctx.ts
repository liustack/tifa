// Ctx stands for the *tifa.Ctx the Go storefront code reads cookies from and sets cookies on.
// A framework makes one from the Cookie header of a request, passes it to the functions that
// take a Ctx, and then adds each of setCookies to the response as a Set-Cookie header. The
// strings are what Go writes for the same cookie, byte for byte.
export interface CookieSpec {
  name: string
  value: string
  path: string
  // maxAge is in seconds. A negative one deletes the cookie and is written as Max-Age=0.
  maxAge: number
  httpOnly?: boolean
  sameSite?: 'Lax'
}

const maxCookies = 3000
const space = /^[ \t\n\r]+|[ \t\n\r]+$/g
const token = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/

// readCookies reads a Cookie header as net/http's readCookies does. Pairs are split at ";" and
// trimmed of spaces. The name, trimmed again, must be a token. A value may be in double quotes,
// which are dropped, and every byte of it must be printable ASCII but not a quote, ";" or a
// backslash. A pair that fails either is skipped, so the first valid cookie of a name wins. A
// header of more than 3000 pairs is read as no cookies at all.
export function readCookies(header: string): Array<[string, string]> {
  if (header.split(';').length > maxCookies) return []
  const out: Array<[string, string]> = []
  for (const raw of header.replace(space, '').split(';')) {
    const part = raw.replace(space, '')
    if (part === '') continue
    const eq = part.indexOf('=')
    const name = (eq < 0 ? part : part.slice(0, eq)).replace(space, '')
    if (!token.test(name)) continue
    let value = eq < 0 ? '' : part.slice(eq + 1)
    if (value.length > 1 && value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1)
    if (![...value].every((ch) => ch >= ' ' && ch < '\x7f' && ch !== '"' && ch !== ';' && ch !== '\\')) continue
    out.push([name, value])
  }
  return out
}

export class Ctx {
  private cookies = new Map<string, string>()
  setCookies: string[] = []

  constructor(cookieHeader?: string | null) {
    for (const [name, value] of readCookies(cookieHeader ?? '')) {
      if (!this.cookies.has(name)) this.cookies.set(name, value)
    }
  }

  cookie(name: string): string | undefined {
    return this.cookies.get(name)
  }

  setCookie(c: CookieSpec): void {
    let s = `${c.name}=${c.value}; Path=${c.path}`
    if (c.maxAge > 0) s += `; Max-Age=${c.maxAge}`
    else if (c.maxAge < 0) s += '; Max-Age=0'
    if (c.httpOnly) s += '; HttpOnly'
    if (c.sameSite) s += `; SameSite=${c.sameSite}`
    this.setCookies.push(s)
    // A later read in the same request sees the new value, as it would not in Go (the request
    // keeps the cookies it came with), so only the response is changed.
  }
}
