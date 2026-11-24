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
	err = renameHeld(exe, filepath.Join(dir, fmt.Sprintf("%s.old-%d%s", base, time.Now().UnixNano(), exeSuffix)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// heldWait bounds how long renameHeld waits for another program to close the file.
const heldWait = 2 * time.Second

// renameHeld is os.Rename, tried again for up to heldWait while another program has the file open
// without sharing its deletion. Windows then refuses the rename (ERROR_SHARING_VIOLATION). Neither
// tifa dev nor the app opens the program, but other programs may read a new one for a moment, such
// as a virus scanner. The rebuild after a save failed this way only in the windows-11-arm runners of
// GitHub Actions, the ones where the real-time protection of Microsoft Defender is on. The go
// command waits 2 seconds for the same reason when it renames or deletes files
// (cmd/internal/robustio). A file held for longer fails as before, with the error of the last try.
func renameHeld(from, to string) error {
	start := time.Now()
	for wait := time.Millisecond; ; wait = min(2*wait, 100*time.Millisecond) {
		err := os.Rename(from, to)
		if err == nil || !heldByAnother(err) || time.Since(start)+wait > heldWait {
			return err
		}
		time.Sleep(wait)
	}
}
