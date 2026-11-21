//go:build windows

package realpath

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// resolve asks Windows for the final name of the file behind a handle: the system follows
// symbolic links and junctions and gives the long name of every element.
func resolve(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return "", err
	}
	// Access 0 is enough to ask for the name, and FILE_FLAG_BACKUP_SEMANTICS lets it open a folder.
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameDOS)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return trimVerbatim(windows.UTF16ToString(buf[:n])), nil
		}
		buf = make([]uint16, n+1) // n is the length needed, with the end
	}
}

// trimVerbatim turns the \\?\C:\dir form the system returns into C:\dir, and \\?\UNC\host\share
// into \\host\share, the forms Go and Node use.
func trimVerbatim(s string) string {
	switch {
	case strings.HasPrefix(s, `\\?\UNC\`):
		return `\` + s[len(`\\?\UNC`):]
	case strings.HasPrefix(s, `\\?\`):
		return s[len(`\\?\`):]
	}
	return s
}

// volumeNameDOS asks GetFinalPathNameByHandle for the drive letter form of the name.
const volumeNameDOS = 0
