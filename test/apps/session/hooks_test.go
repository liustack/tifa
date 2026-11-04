package sessiontest

import (
	"path/filepath"
	"testing"

	"github.com/liustack/tifa/tifatest"
)

func TestFileStoreIsAValidStore(t *testing.T) {
	tifatest.TestSessionStore(t, &fileStore{path: filepath.Join(t.TempDir(), "sessions.json")})
}
