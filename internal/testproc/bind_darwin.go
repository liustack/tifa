//go:build darwin

package testproc

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

var (
	// mu orders the starts, since each one changes how this process handles SIGHUP for a moment.
	mu sync.Mutex
	// masters are the master sides of the terminals the programs run on. They stay open for as long
	// as the process lives, and the system closes them when it ends, however it ends. A master
	// that is garbage collected would hang up its program, so the list keeps them.
	masters []*os.File
)

// start makes macOS stop cmd when this process ends. macOS has no signal for the death of a parent,
// but it hangs up a terminal when the master side of it closes, and sends SIGHUP to the process
// that has it as its controlling terminal and to its process group. So cmd runs as the leader of
// a session of its own, on a terminal whose master side only this process holds.
func start(cmd *exec.Cmd) error {
	master, slave, err := openPty()
	if err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	cmd.SysProcAttr.Ctty = 3 + len(cmd.ExtraFiles) // the descriptor of the slave in the program
	cmd.ExtraFiles = append(cmd.ExtraFiles, slave)

	mu.Lock()
	defer mu.Unlock()
	// A program started with SIGHUP ignored, as nohup does, keeps it ignored. A signal that this
	// process handles is back to its default in the programs it starts, so it handles SIGHUP
	// while it starts one, and gives the signal to itself afterwards if one came.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	err = cmd.Start()
	signal.Stop(hup)
	slave.Close()
	if len(hup) > 0 {
		syscall.Kill(os.Getpid(), syscall.SIGHUP)
	}
	if err != nil {
		master.Close()
		return err
	}
	masters = append(masters, master)
	return nil
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
