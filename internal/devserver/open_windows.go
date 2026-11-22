package devserver

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// openToHash opens the file name for reading while the watcher hashes it. It opens it as os.Open
// does, and also shares delete: Windows refuses to rename a file over one that a handle without
// it holds open, or to delete it, and the watcher hashes a file right when it was saved, so an
// editor that saves again by renaming a new file over it, as many do, would fail with "Access is
// denied".
func openToHash(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(longPath(name))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	// FILE_FLAG_BACKUP_SEMANTICS opens a directory too, as os.Open does.
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}

// longPath gives an absolute path of 248 characters or more the \\?\ prefix, which lets Windows
// open it when long paths are not enabled, as os.Open does.
func longPath(name string) string {
	if len(name) < 248 || !filepath.IsAbs(name) || strings.HasPrefix(name, `\\?\`) {
		return name
	}
	if rest, ok := strings.CutPrefix(name, `\\`); ok {
		return `\\?\UNC\` + rest
	}
	return `\\?\` + name
}
