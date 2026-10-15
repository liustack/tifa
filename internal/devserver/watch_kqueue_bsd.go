//go:build dragonfly || freebsd || netbsd || openbsd

package devserver

import "golang.org/x/sys/unix"

// openWatch opens a file to watch it. Non-blocking, so a FIFO that took the place of a file
// does not hang the watcher.
const openWatch = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC
