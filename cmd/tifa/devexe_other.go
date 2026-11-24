//go:build !windows

package main

// heldByAnother reports whether err says that another program holds the file. Only Windows refuses
// a rename for that.
func heldByAnother(error) bool { return false }
