//go:build unix

package e2e

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// killWithGroup puts the command in its own process group and makes the deadline kill the group.
func killWithGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// endProcessTree kills the process pid, a daemon that a test left behind, and everything below
// it, which is the Chrome with its helpers.
func endProcessTree(pid int) {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		c, err1 := strconv.Atoi(f[0])
		p, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			children[p] = append(children[p], c)
		}
	}
	var kill func(int)
	kill = func(p int) {
		for _, c := range children[p] {
			kill(c)
		}
		syscall.Kill(p, syscall.SIGKILL)
	}
	kill(pid)
}
