package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// heldByAnother reports whether err says that another program has the file open without sharing
// its deletion, which a rename needs too.
func heldByAnother(err error) bool { return errors.Is(err, windows.ERROR_SHARING_VIOLATION) }
