declare const global: typeof globalThis

globalThis.result = `${process.env.NODE_ENV}:${global === globalThis}`
