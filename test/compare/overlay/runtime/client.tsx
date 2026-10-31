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
