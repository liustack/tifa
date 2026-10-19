package bundle

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/klauspost/compress/gzhttp"
	"github.com/klauspost/compress/gzip"
)

// precompressed are the extensions of the files precompress compresses: the types that the
// server compresses (compressible in server), as far as Go knows them without the system's
// MIME files, so every server sends them with the same type.
var precompressed = []string{".js", ".mjs", ".css", ".html", ".htm", ".json", ".svg", ".xml", ".wasm"}

// precompress writes name.gz next to every file under dir with an extension of precompressed,
// from gzhttp's threshold on, at gzip's best level. The server sends it to clients that accept
// gzip, as nginx's gzip_static does, and spends no CPU on these files. A file that gzip does not
// make smaller gets none. A missing dir, as for an application without pages, holds nothing.
func precompress(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !slices.Contains(precompressed, filepath.Ext(path)) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) < gzhttp.DefaultMinSize {
			return err
		}
		var out bytes.Buffer
		w, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		if out.Len() >= len(data) {
			return nil
		}
		return os.WriteFile(path+".gz", out.Bytes(), 0o644)
	})
}
