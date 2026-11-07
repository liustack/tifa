// The method handlers of the storefront, answering as tifa does. The answers themselves are made
// by test/compare/domain/store/calls.ts, which the three apps share. This file is the part of
// React Router: a route of it has a loader for GET and an action for every other method, and a
// resource route is needed for each write.
//
// The handlers are resource routes under /store/<lang>/_write, since a route of React Router that has a page answers a POST
// or a PATCH to its URL with the page. The shop's URL /store/cart for the PATCH of the cart
// becomes /store/zh-CN/_write/cart here (see overlay/runtime/call.ts).

import { callMine, methodNotAllowed } from '../../../domain/store/calls.ts'

type Params = Record<string, string | undefined>
type Method = (request: Request, params: never) => Response | Promise<Response>

// allowed is the Allow header of a 405, as Tifa writes it: GET and HEAD first when the URL has a page
// or a GET handler, then the other methods.
function allowed(methods: Record<string, Method>, pages: boolean) {
  const names = Object.keys(methods).filter((m) => m !== 'GET')
  return (pages || 'GET' in methods ? ['GET', 'HEAD', ...names] : names).join(', ')
}

// handlers makes the loader and the action of a resource route: GET goes to the loader and every
// other method to the action. pages says the URL of the route has a page in Tifa.
export function handlers(methods: Record<string, Method>, pages = false) {
  const run = async ({ request, params }: { request: Request; params: Params }) => {
    const method = methods[request.method]
    if (!method) return methodNotAllowed(allowed(methods, pages))
    return method(request, params as never)
  }
  return { loader: run, action: run }
}

// mine is the GET of app/store/mine.
export const mine = callMine
