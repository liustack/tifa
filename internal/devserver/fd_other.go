//go:build !unix

package devserver

func raiseFileLimit() bool { return false }

func fileLimit() uint64 { return 0 }
