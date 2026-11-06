// Ported from cart/recent.go: values by key in memory, at most limit of them, forgotten when
// not used for longer than idle. When it is full, the least recently used goes first. Go needs a
// lock around it. Node runs one request at a time between awaits, and none of these calls waits.
export class Recent<V> {
  private items = new Map<string, { value: V; used: number }>() // oldest use first
  limit: number
  idle: number

  constructor(limit: number, idle: number) {
    this.limit = limit
    this.idle = idle
  }

  // get returns the value of key and counts it as used at now. A value idle for too long is
  // forgotten and not returned.
  get(key: string, now: number): V | undefined {
    const e = this.items.get(key)
    if (!e) return undefined
    this.items.delete(key)
    if (now - e.used > this.idle) return undefined
    e.used = now
    this.items.set(key, e)
    return e.value
  }

  // has reports whether key holds a value, without counting it as used.
  has(key: string): boolean {
    return this.items.has(key)
  }

  // add stores value under key as used at now. It then forgets the values idle for too long, and
  // the least recently used ones past the limit.
  add(key: string, value: V, now: number): void {
    this.items.delete(key)
    this.items.set(key, { value, used: now })
    for (const [k, e] of this.items) {
      if (this.items.size <= this.limit && now - e.used <= this.idle) break
      this.items.delete(k)
    }
  }

  get size(): number {
    return this.items.size
  }
}
