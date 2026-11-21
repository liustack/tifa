package server

import "mime"

// fileTypes are the Content-Type values of the files the front end and public/ commonly hold.
// They are Go's own table, pinned: the mime package lets the system override them, with the
// registry on Windows and /etc/mime.types elsewhere, and a machine whose entry for .js says
// application/javascript would change what the browser is sent.
var fileTypes = map[string]string{
	".avif": "image/avif",
	".css":  "text/css; charset=utf-8",
	".gif":  "image/gif",
	".htm":  "text/html; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json",
	".mjs":  "text/javascript; charset=utf-8",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".wasm": "application/wasm",
	".webp": "image/webp",
	".xml":  "text/xml; charset=utf-8",
}

func init() { pinFileTypes() }

// pinFileTypes sets fileTypes over whatever the system says.
func pinFileTypes() {
	for ext, typ := range fileTypes {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			panic(err) // the table is constant: a bad entry is a mistake of this file
		}
	}
}
