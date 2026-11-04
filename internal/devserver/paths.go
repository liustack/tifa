package devserver

// Paths the dev server answers itself. Everything else goes to the app.
const (
	devPrefix  = "/__tifa/dev/"
	eventsPath = devPrefix + "events"
	clientPath = devPrefix + "client.js"
	workerPath = devPrefix + "worker.js"
)

// reloadPath is where the dev server asks the running app to load the SSR bundle (?bundle=1), the
// content (?content=1) or both again, after it rebuilt only those. server.DevMain answers it. When
// the content fails to load, the answer names it in reloadFailedHeader. Browser requests under
// devPrefix never reach the app.
const (
	reloadPath         = devPrefix + "reload"
	reloadFailedHeader = "X-Tifa-Reload-Failed"
)

// VitePath is where Vite's dev server serves modules, its base. Requests under it go to Vite.
const VitePath = "/__tifa/vite/"
