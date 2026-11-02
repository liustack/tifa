// The module "tifa/client" for the comparison apps: clientOnly is tifa's own (copied from the
// shop's generated code), TifaError is what a handler function throws, RouteError what the Error
// components of the shop's routes receive.
export { clientOnly } from './client-only'

export class TifaError extends Error {
  readonly status: number
  readonly fields: Record<string, string>
  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'TifaError'
    this.status = status
    this.fields = fields
  }
}

export class RouteError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'RouteError'
    this.status = status
  }
}

// The session of tifa/client, for the shop's own app/account.tsx and app/layout.tsx. The comparison
// runs the shop open, with nobody signed in. Session.In and Session.Out are spans with the
// data-session attribute, as tifa's, and both are in the page, since tifa renders both and a style
// hides one.
export function useSession(): { signedIn: boolean; data: { name: string; initials: string } | undefined; ready: boolean } {
  return { signedIn: false, data: undefined, ready: true }
}

type SpanProps = React.HTMLAttributes<HTMLSpanElement>

export const Session = {
  In: (props: SpanProps) => <span data-session="in" {...props} />,
  Out: (props: SpanProps) => <span data-session="out" {...props} />,
}
