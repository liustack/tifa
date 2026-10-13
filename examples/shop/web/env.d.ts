declare module '*.css'
declare module 'whatwg-fetch' {
  export const Headers: { new (init?: HeadersInit): globalThis.Headers }
  export const Response: {
    new (body?: BodyInit | null, init?: ResponseInit): globalThis.Response
  }
}
