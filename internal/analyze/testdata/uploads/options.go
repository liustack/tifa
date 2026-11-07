package uploads

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	UploadLimit = tifa.Size("upload.limit", 8<<20, "Largest upload")
	UploadTime  = tifa.Duration("upload.time", 5*time.Minute, "Time an upload may take")
)
