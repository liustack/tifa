//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd)

package devserver

import "github.com/fsnotify/fsnotify"

// watchesFiles reports that a directory watch reports the edits of its files too, as inotify
// does: files take no watch of their own.
const watchesFiles = false

// newSystemWatcher returns fsnotify's watcher: inotify on Linux.
func newSystemWatcher() (*systemWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &systemWatcher{add: fw.Add, remove: fw.Remove, close: fw.Close, events: fw.Events, errors: fw.Errors}, nil
}
