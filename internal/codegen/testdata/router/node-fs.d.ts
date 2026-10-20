// The two functions of node:fs that router.test.tsx and head.test.tsx use to pass a server page
// from one to the other. The packages the tests borrow have no @types/node.
declare module 'node:fs' {
  export function readFileSync(path: string, encoding: 'utf8'): string
  export function writeFileSync(path: string, data: string): void
}
