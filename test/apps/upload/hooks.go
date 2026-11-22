package uploads

import (
	"context"
	"os"
	"time"

	"github.com/liustack/tifa"
)

func OnStart(context.Context) error {
	return os.MkdirAll(UploadDir, 0o755)
}

// parsing lets two uploads read their form at a time. The others wait here, before their body is
// read, so a burst of uploads does not hold the memory of all of them at once.
var parsing = make(chan struct{}, 2)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Request().Method == "POST" && c.Pattern() == "/photos" {
		// This route lets a request be as large as upload.limit, and gives a phone on a slow line
		// the time to send a large picture. The other routes keep server.body_limit.
		if err := c.SetBodyLimit(int64(UploadLimit)); err != nil {
			return err
		}
		if err := c.SetReadTimeout(5 * time.Minute); err != nil {
			return err
		}
		select {
		case parsing <- struct{}{}:
			defer func() { <-parsing }()
		case <-c.Context().Done():
			return c.Context().Err()
		}
	}
	return next(c)
}
