package devserver

// Paths the dev server answers itself. Everything else goes to the app.
const (
	devPrefix  = "/__tifa/dev/"
	eventsPath = devPrefix + "events"
	clientPath = devPrefix + "client.js"
	workerPath = devPrefix + "worker.js"
)

// VitePath is where Vite's dev server serves modules, its base. Requests under it go to Vite.
const VitePath = "/__tifa/vite/"
