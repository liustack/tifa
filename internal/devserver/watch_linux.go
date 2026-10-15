package devserver

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// inotify takes one watch per directory from fs.inotify.max_user_watches, a limit shared by every
// process of the user, editors included.

// watchBudget returns half of the user's inotify watches, so editors and other tools keep some.
func watchBudget() int {
	data, err := os.ReadFile("/proc/sys/fs/inotify/max_user_watches")
	if err != nil {
		return math.MaxInt
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return math.MaxInt
	}
	return n / 2
}

// outOfWatches reports whether err means the user has no inotify watch or instance left, or the
// process no file descriptor.
func outOfWatches(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}
