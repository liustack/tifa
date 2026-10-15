package devserver

// Paths the dev server answers itself. Everything else goes to the app.
const (
	devPrefix  = "/__tifa/dev/"
	eventsPath = devPrefix + "events"
	clientPath = devPrefix + "client.js"
	workerPath = devPrefix + "worker.js"
)

// reloadPath is where the dev server asks the running app to load the SSR bundle again, after it
// rebuilt only that bundle. server.DevMain answers it. Browser requests under devPrefix never
// reach the app.
const reloadPath = devPrefix + "reload"

// VitePath is where Vite's dev server serves modules, its base. Requests under it go to Vite.
const VitePath = "/__tifa/vite/"
