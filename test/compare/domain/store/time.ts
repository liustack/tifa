// iso writes an instant (milliseconds since the epoch) as Go's time.Time does for a time in UTC:
// 2026-09-02T10:00:00Z. Whole seconds have no fraction.
export function iso(ms: number): string {
  return new Date(ms).toISOString().replace('.000Z', 'Z')
}

export const HOUR = 3_600_000
export const DAY = 24 * HOUR
export const MINUTE = 60_000
