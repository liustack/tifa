//go:build windows

package devserver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has neither process groups that a signal reaches nor a signal that one program can send
// another to ask it to stop. So the app runs in a job object of its own, which Windows uses to treat
// a process and all it starts as one, and the stop request travels on the lifeline: tifa dev closes
// the app's standard input, which the app also reads the end of when tifa dev dies, and
// server.DevMain stops as it does on SIGTERM, running OnShutdown. A console event (CTRL_BREAK) was
// the other way, but it needs a console that tifa dev and the app share, which a tifa dev started
// without one (by a service, an editor or an agent) does not have, and the pipe works the same
// with a console or without.

// supported says whether Run runs on this system.
const supported = true

// ownGroup starts the app suspended in a process group of its own. The group keeps Ctrl-C in the
// console away from the app, as Setpgid does on Unix: tifa dev stops it. Suspended, the app runs
// no code before joinGroup put it in its job, so nothing it starts escapes the job.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
}

// group is the job object of the app. The job kills its processes when its last handle closes, so
// the app and what it started also end when tifa dev ends, however it ends.
type group struct {
	mu  sync.Mutex
	job windows.Handle // 0 once closed: a closed handle's value may name another object by now
}

// joinGroup puts the suspended process p in a new job and lets it run.
func joinGroup(p *os.Process) (*group, error) {
	job, err := newJob()
	if err != nil {
		return nil, err
	}
	g := &group{job: job}
	// The pid is the app's: os.Process holds a handle to it, so the number is not given again.
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		g.close()
		return nil, fmt.Errorf("opening the app's process: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		g.close()
		return nil, fmt.Errorf("putting the app in a job object: %w", err)
	}
	if err := resume(uint32(p.Pid)); err != nil {
		g.close()
		return nil, err
	}
	return g, nil
}

// newJob makes a job object that kills its processes when its last handle closes. The handle is not
// inherited: only this process holds it.
func newJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return job, nil
}

// resume lets the threads of the suspended process pid run. A process made with CREATE_SUSPENDED
// has one, its main thread.
func resume(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("listing the threads of the app: %w", err)
	}
	defer windows.CloseHandle(snap)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := 0
	for err = windows.Thread32First(snap, &entry); err == nil; err = windows.Thread32Next(snap, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("resuming the app: %w", err)
		}
		_, err = windows.ResumeThread(th)
		windows.CloseHandle(th)
		if err != nil {
			return fmt.Errorf("resuming the app: %w", err)
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("listing the threads of the app: %w", err)
	}
	if resumed == 0 {
		return errors.New("resuming the app: it has no thread")
	}
	return nil
}

func (g *group) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		windows.CloseHandle(g.job)
		g.job = 0
	}
}

// interruptGroup asks the app to stop by closing its lifeline. Only the app reads it: what the app
// started is ended by killGroup once the app exited, as on Unix.
func interruptGroup(a *app) error {
	a.life.w.Close()
	return nil
}

// killGroup ends every process of the job. Once the job is closed its processes are gone already.
func killGroup(a *app) error {
	a.grp.mu.Lock()
	defer a.grp.mu.Unlock()
	if a.grp.job == 0 {
		return os.ErrProcessDone
	}
	return windows.TerminateJobObject(a.grp.job, 1)
}

// lifelineFD is the file descriptor of the lifeline in the app: its standard input. Windows passes
// a program no other files by number.
const lifelineFD = 0

// lifelineFlag is the flag that tells server.DevMain where the lifeline is.
const lifelineFlag = "--lifeline-fd"

// lifeline is a pipe that tells the app whether tifa dev still lives and wants it to run. The app
// holds the read end as its standard input and tifa dev the write end, which neither side writes
// to. The system closes the write end when tifa dev ends, however it ends, and tifa dev closes it
// to stop the app. Either way the app reads the end of the file and stops.
type lifeline struct{ r, w *os.File }

// newLifeline makes cmd pass the read end of a new pipe on as its standard input, and tell the
// app where it is.
func newLifeline(cmd *exec.Cmd) (*lifeline, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = r
	cmd.Args = append(cmd.Args, lifelineFlag, "0")
	return &lifeline{r, w}, nil
}

// started closes the read end tifa dev holds, once the app has its own.
func (l *lifeline) started() { l.r.Close() }

// close closes both ends, once the app has exited, or never started.
func (l *lifeline) close() {
	l.r.Close()
	l.w.Close()
}

// BindChildren puts this process in a new job object that kills its processes when its last
// handle closes. Every process it starts from then on is in the job too: the driver of Vite on
// Node, go build, the app (in a job of its own inside this one) and what they start. Only this
// process holds the handle, so when it ends, however it ends (killed in the Task Manager, its
// console window closed), the system closes the handle and ends them all. tifa dev calls it before
// it starts anything.
func BindChildren() error {
	job, err := newJob()
	if err != nil {
		return err
	}
	// The handle stays open as long as the process lives.
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("AssignProcessToJobObject for tifa dev: %w", err)
	}
	return nil
}
