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

export class Ctx {
  private cookies = new Map<string, string>()
  setCookies: string[] = []

  constructor(cookieHeader?: string | null) {
    // As net/http reads a Cookie header: pairs split at ";", the first of a name wins, a value
    // in double quotes loses them.
    for (const part of (cookieHeader ?? '').split(';')) {
      const pair = part.trim()
      const eq = pair.indexOf('=')
      if (eq <= 0) continue
      const name = pair.slice(0, eq)
      let value = pair.slice(eq + 1)
      if (value.length > 1 && value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1)
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
