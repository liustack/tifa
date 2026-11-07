// Package uploads is the root package of the app: its settings and hooks.
package uploads

import "github.com/liustack/tifa"

var (
	// UploadLimit is the largest request body of an upload.
	UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")
	// UploadDir is where uploaded files are kept, relative to the folder the app runs in.
	UploadDir = tifa.String("upload.dir", "data/uploads", "Folder for uploaded files")
)
