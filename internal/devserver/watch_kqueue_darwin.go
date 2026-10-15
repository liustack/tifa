package devserver

import "golang.org/x/sys/unix"

// openWatch opens a file only to watch it, so it does not keep its volume from unmounting.
const openWatch = unix.O_EVTONLY | unix.O_CLOEXEC
