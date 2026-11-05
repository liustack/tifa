// NotFound is what a Layout or a Load throws for an unknown project or order, as Go returns
// tifa.ErrNotFound. A framework turns it into a 404.
export class NotFound extends Error {}

// Invalid is what a write throws for a recipient the shop would not take: the fields that fail.
export class Invalid extends Error {
  fields: string[]
  constructor(fields: string[]) {
    super('invalid: ' + fields.join(', '))
    this.fields = fields
  }
}

// Locked is what a write throws when the order has shipped, as Go returns ErrRecipientLocked.
export class Locked extends Error {}
