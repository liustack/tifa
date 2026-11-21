package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// moveAside renames the program at exe to <name>.old-<time><suffix> in the same folder, where go
// build can then write the new one, and deletes the programs earlier calls moved aside. On Windows
// the app of tifa dev runs from exe while go build writes its next version, and Windows refuses to
// write over or delete a program that runs, but lets it be renamed. The app keeps running from the
// new name until tifa dev stops it, and the next call deletes that file. A file Windows refuses to
// delete runs still, and is left for a later call. exe not being there is no error: the first build
// makes it, and a failed build leaves none.
func moveAside(exe string) error {
	dir, name := filepath.Split(exe)
	base := strings.TrimSuffix(name, exeSuffix)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), base+".old-") && strings.HasSuffix(e.Name(), exeSuffix) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrPermission) {
				return err
			}
		}
	}
	err = os.Rename(exe, filepath.Join(dir, fmt.Sprintf("%s.old-%d%s", base, time.Now().UnixNano(), exeSuffix)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
