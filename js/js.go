// Package js holds the JavaScript runtime sources of tifa. The tifa CLI serves them or writes
// them into generated code, applications do not import this package.
package js

import _ "embed"

// DevClient runs in the browser during tifa dev: it reloads the page after each rebuild and shows
// build errors in an overlay. The dev client entry imports it, it also works as a plain script.
//
//go:embed dev/client.js
var DevClient string

// DevWorker is the SharedWorker that gives every tab of a browser one shared SSE connection.
//
//go:embed dev/worker.js
var DevWorker string
