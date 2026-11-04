package uploads

import (
	"context"
	"os"
)

func OnStart(context.Context) error {
	return os.MkdirAll(UploadDir, 0o755)
}
