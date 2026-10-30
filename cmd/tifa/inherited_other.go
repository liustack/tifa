//go:build !unix

package main

// closeInheritedOnExec does nothing here: other systems pass no files on to the programs that a
// process starts, apart from the ones it names.
func closeInheritedOnExec() error { return nil }
