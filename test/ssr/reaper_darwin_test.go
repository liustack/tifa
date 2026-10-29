//go:build darwin

package ssr_test

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"
)

// ptyMaster is the master side of the terminal that cmd runs on. It stays open for as long as
// the reaper lives, and the system closes it when the reaper ends, however it ends.
var ptyMaster *os.File

// bindToReaper makes macOS kill cmd when the reaper ends, however it ends. macOS has no signal for
// the death of a parent, but it hangs up a terminal when the master side of it closes, and
// sends SIGHUP to the process that has it as its controlling terminal. So cmd runs as the leader
// of a session of its own, on a terminal whose master side only the reaper holds.
// The returned function is for the reaper to call once cmd has started.
func bindToReaper(cmd *exec.Cmd) (started func(), err error) {
	master, slave, err := openPty()
	if err != nil {
		return nil, err
	}
	ptyMaster = master
	// A program started with SIGHUP ignored, as nohup does, keeps it ignored: a signal that the
	// reaper handles is back to its default in the programs it starts.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
	cmd.ExtraFiles = []*os.File{slave} // file descriptor 3
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 3}
	return func() { slave.Close() }, nil
}

// Requests of ioctl for a terminal, from sys/ttycom.h.
const (
	tiocPtyGrant = 0x20007454
	tiocPtyUnlk  = 0x20007452
	tiocPtyGname = 0x40807453
)

// openPty opens a terminal and returns its two sides, as posix_openpt does.
func openPty() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	var name [128]byte
	for _, call := range []struct {
		request uintptr
		arg     uintptr
	}{{tiocPtyGrant, 0}, {tiocPtyUnlk, 0}, {tiocPtyGname, uintptr(unsafe.Pointer(&name[0]))}} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), call.request, call.arg); errno != 0 {
			master.Close()
			return nil, nil, fmt.Errorf("open a terminal: ioctl %#x: %w", call.request, errno)
		}
	}
	path := string(name[:])
	for i, b := range name {
		if b == 0 {
			path = string(name[:i])
			break
		}
	}
	slave, err = os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
