declare const __SSR_PROFILE__: boolean

globalThis.result = 'rendered'
if (__SSR_PROFILE__) globalThis.renderProfile = { start: globalThis.__ssrNow() }
