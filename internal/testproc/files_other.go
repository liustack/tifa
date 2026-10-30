//go:build !unix

package testproc

// CloseInheritedOnExec does nothing here: other systems pass no files on to the programs that a
// process starts, apart from the ones it names.
func CloseInheritedOnExec() error { return nil }
