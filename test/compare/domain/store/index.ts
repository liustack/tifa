// The Go code of the storefront (examples/shop/app/store), ported function by function. A
// framework calls these once for each request that needs the data, as tifa runs the Go ones.
// Names are the Go names in camelCase. TestStoreDomainMatchesGo keeps the answers equal to Go's.
//
// Pages. Each page's data is its Layout and Load results, by route id (routes.ts has loadRoute(id, ...)).
// lang is the language of the URL: zh-CN for /store, en-US for /en-US/store. An unknown one throws NotFound from every
// function that calls findLocale, which a framework turns into a 404. c is a Ctx made from the
// Cookie header of the request (ctx.ts), query is its URLSearchParams.
//
//   URL                                    route id                                   function
//   /store   (Layout, all below)    app/store/layout                     layoutLang(params)
//   /store                          app/store/route                      loadHome(params)
//   /store/products                 app/store/products/route             loadList(params, query)
//   /store/products/{product}       app/store/products/product+/route    loadProduct(params, query)
//   /store/cart                     app/store/cart/route                 loadCart(c, params)
//   /store/checkout[?placed=SS-..]  app/store/checkout/route             loadCheckout(c, params, query)
//   /store/journal                  app/store/journal/route              loadJournal(params)
//   /store/journal/{post}           app/store/journal/post+/route        loadPost(params)
//   GET /store/mine  (JSON)         app/store/mine/route (GET)           getMine(c, params)
//
// A page that has a Layout above it gets that Layout's data too: every page has layoutLang.
// The root Layout of the workbench (loadShell) does not run for the storefront.
//
// Writes. Each has a Request to Response function in writes.ts that answers as tifa does (JSON
// body, status, Set-Cookie), and the plain function it calls. Use the first to serve the route
// as it is. All of them need the cookies of the request and add to the cookies of the answer:
//
//   POST  /store/products/{product}  {color, size, quantity}  postProductRoute -> postProduct  200 {count}
//   PATCH /store/cart                {line, quantity}         patchCartRoute   -> patchCart    200 Cart
//   POST  /store/cart                {code}                   postCartRoute    -> postCoupon   204
//   POST  /store/checkout            Order                    postCheckoutRoute -> postCheckout 303 to /store/checkout?placed={id}
//   POST  /store/mine                {slug}                   postMineRoute    -> postMine     204
//
// Errors: NotFound is 404, Fields is 422 {"error", "fields"}, HttpError is its own status
// ({"error": message}, 409 for an empty or forgotten cart), a body that is not JSON of the right
// shape is 400, and a body that is not application/json is 415.
//
// Cookies. store_cart (the id of the visitor's cart, in the memory of the process) and shop_seen
// (the slugs of the products looked at, joined by dots) are written as Go writes them, with
// Path=/, Max-Age, HttpOnly and SameSite=Lax. shop_seen works between a Go and a Node
// server. store_cart does not: the carts live in each process, so a cart id the other server
// made is an unknown id here, which Go treats the same way.
//
// Times. Delivery and checkout functions take now (milliseconds) so a test can fix it. Instants
// are written as ISO 8601 text, in UTC. The pages format them in the locale's time zone.
export * from './errors.ts'
export * from './ctx.ts'
export * from './store.ts'
export * from './home.ts'
export * from './product.ts'
export * from './checkout.ts'
export * from './mine.ts'
export * from './routes.ts'
export * from './writes.ts'
export * from './products/index.ts'
export * from './cart/index.ts'
export * from './journal/route.ts'
export * from './journal/post.ts'
export * from './calls.ts'
