import { languageOf } from '@/runtime/languages'
import { callMine, callSeen } from '../../../domain/store/calls.ts'
import { handlers } from '../compare/store-wire'

// GET and POST of app/store/mine: the cart and the products seen, and remembering one. The
// language is the one in the URL, /store/mine or /zh-CN/store/mine.
const lang = (request: Request) => ({ lang: languageOf(new URL(request.url).pathname) })
const route = handlers({ GET: (request) => callMine(request, lang(request)), POST: (request) => callSeen(request, lang(request)) })
export const loader = route.loader
export const action = route.action
