// The module the generated +route modules send their requests through. The comparison apps show
// pages and never call a handler, so a call says so.
export interface CallOptions {
  signal?: AbortSignal
}

export function segment(value: string) {
  return encodeURIComponent(value)
}

export async function call(method: string, path: string, _options: CallOptions & { input?: unknown }, _kind: string): Promise<never> {
  throw new Error(`${method} ${path}: the comparison apps have no handlers`)
}
