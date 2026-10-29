// Counters of the work a request does, which the comparison reads to prove that every page
// request computes its data and renders again. They live on globalThis, since a framework may
// load this module once for its server components and once for its route handlers.

export interface Counts {
  ordersOf: number // calls of ordersOf, the filter, copy and sort every list, detail and stats query starts with
  shell: number // runs of the root Layout
  project: number // runs of the project Layout
  page: number // runs of the Load of a page
  renders: number // renders of the page, counted by the apps
}

const g = globalThis as typeof globalThis & { __compareCounts?: Counts }

export const counts: Counts = (g.__compareCounts ??= { ordersOf: 0, shell: 0, project: 0, page: 0, renders: 0 })
