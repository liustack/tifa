//go:build unix

package devserver

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// ownGroup puts the app in a process group of its own, so stopping it reaches every process it started.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func interruptGroup(p *os.Process) error { return signalGroup(p, syscall.SIGINT) }

func killGroup(p *os.Process) error { return signalGroup(p, syscall.SIGKILL) }

// signalGroup signals the process group led by p. It returns os.ErrProcessDone when the group is empty.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// lifelineFD is the file descriptor of the lifeline in the app: the first file after stdin, stdout
// and stderr that cmd.ExtraFiles passes on.
const lifelineFD = 3

// lifelineFlag is the flag that tells server.DevMain where the lifeline is.
const lifelineFlag = "--lifeline-fd"

// lifeline is a pipe that tells the app whether tifa dev still lives. The app holds the read end
// and tifa dev the write end, which neither side writes to. The system closes the write end when
// tifa dev ends, however it ends, SIGKILL included, which runs no code, and the app reads the end
// of the file and stops. Linux can send a signal when the parent dies and macOS cannot, so the pipe
// is the one way that works on both without help from the runtime. The app is in a process group of
// its own, which the hangup of a terminal does not reach.
type lifeline struct{ r, w *os.File }

// newLifeline makes cmd pass the read end of a new pipe on as file descriptor lifelineFD, and tell
// the app where it is.
func newLifeline(cmd *exec.Cmd) (*lifeline, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = append([]*os.File{r}, cmd.ExtraFiles...)
	cmd.Args = append(cmd.Args, lifelineFlag, strconv.Itoa(lifelineFD))
	return &lifeline{r, w}, nil
}

// started closes the read end tifa dev holds, once the app has its own.
func (l *lifeline) started() { l.r.Close() }

// close closes the write end, once the app has exited, or never started. Until then it must stay
// open, and referenced, since a file closes when it is garbage collected.
func (l *lifeline) close() {
	l.r.Close()
	l.w.Close()
}
