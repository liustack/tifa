import type { ReactNode } from 'react'

export function Badge({ children }: { children: ReactNode }) {
  return <span className="rounded-full bg-sky-100 px-2 py-0.5 text-sm text-sky-800">{children}</span>
}
