//go:build windows

package bundle

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// probe hack: filepath.EvalSymlinks does not follow junctions since Go 1.23, which pnpm makes.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	s := windows.UTF16ToString(buf[:n])
	return strings.TrimPrefix(s, `\\?\`), nil
}
