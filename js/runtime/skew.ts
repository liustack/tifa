// A page left open in a browser while the server was replaced by a build from other sources. The
// page sends the server a request that build refuses with 409 (the data of a navigation in
// data.ts, a handler function in call.ts), or asks for a code chunk the new build no longer has
// (Vite's vite:preloadError). Each of these reloads the page, once for the build it is stale
// against, so a page the reload brings back stale again shows the failure instead of reloading
// in a loop.

// reloaded is the key in sessionStorage of the reload made for a target.
const reloaded = (target: string) => 'tifa-reload:' + target

// reloading is set once this page has started a reload. The page is going away, so every later
// signal waits for the reload instead of starting another.
let reloading = false

// reloadForBuild reloads the page unless it was reloaded for target before, and reports whether
// the reload is under way. target names the build the page is stale against: the build the
// server reported, or for a chunk the page's own build. A false return means there is no reload:
// either the page reloaded for this target already and the reload did not fix it, or the mark of
// the reload cannot be kept. The caller then fails as it would without this module, and its error
// says to reload the page.
//
// sessionStorage is the only memory, since it survives a reload of the tab and belongs to the
// site. The mark is written and read back before the page reloads. When the storage is blocked,
// full or does not keep what it was given, a reload could repeat for as long as the build keeps
// disagreeing, so the page does not reload and leaves that to the visitor.
export function reloadForBuild(target: string): boolean {
  if (reloading) return true
  try {
    const key = reloaded(target)
    const storage = window.sessionStorage
    if (storage.getItem(key) !== null) return false
    storage.setItem(key, '1')
    if (storage.getItem(key) === null) return false
  } catch {
    return false
  }
  reloading = true
  window.location.reload()
  return true
}

// resetReloads gives the page the memory of a page that has just loaded. For tests, which run a
// page's reload in the same process.
export function resetReloads() {
  reloading = false
}

// watchChunks reloads the page when a dynamic import fails, which a new build causes by taking
// the old chunk away. Vite sends vite:preloadError before the import fails, and an event that
// is canceled makes the import resolve without a module instead of failing. The reload is for
// the build the page has, as the build the server runs is not known here. build returns it.
//
// A page that has reloaded already leaves the event alone, so the import fails and the error
// reaches the Error of the route or the code that imported. TanStack Router reloads a page by
// itself, once for each message, when the module of a lazy route is missing. That reload would
// be a second one, or one the page could not keep a mark of, so the message of the error changes
// to one the router does not know, and says to reload the page.
export function watchChunks(build: () => string) {
  window.addEventListener('vite:preloadError', (event) => {
    if (reloadForBuild('chunk:' + build())) {
      event.preventDefault()
      return
    }
    const error = (event as Event & { payload?: unknown }).payload
    if (error instanceof Error) error.message = `The application was updated, reload the page (${error.message})`
  })
}
