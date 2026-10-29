// NotFound is what a Layout or a Load throws for an unknown project or order, as Go returns
// tifa.ErrNotFound. A framework turns it into a 404.
export class NotFound extends Error {}
