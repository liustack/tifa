//go:build !windows

package bundle

import "path/filepath"

func realPath(p string) (string, error) { return filepath.EvalSymlinks(p) }
