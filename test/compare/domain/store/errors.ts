// What a Go Load or write handler returns as an error. A framework turns each into the answer
// tifa gives: NotFound is 404, Fields is 422 with the messages as "fields", HttpError is its
// own status with its message. writes.ts does that for the write routes.
export { NotFound } from '../errors.ts'

// Fields is a message per input field, by its json name, as tifa.Fields.
export class Fields extends Error {
  fields: Record<string, string>
  constructor(fields: Record<string, string>) {
    super('invalid fields: ' + Object.keys(fields).sort().join(', '))
    this.fields = fields
  }
}

// HttpError is a tifa.Error: a status and a message the visitor may read.
export class HttpError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}
