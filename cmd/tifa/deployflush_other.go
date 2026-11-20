//go:build !linux && !darwin

package main

import "os"

// flushInput throws away the keys the terminal holds and nobody read. Setup can take seconds to plan,
// and a developer who presses Enter meanwhile must not answer the question that comes after. On this
// system it does nothing.
func flushInput(*os.File) {}
