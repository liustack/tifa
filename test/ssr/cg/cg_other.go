//go:build !(linux && amd64)

package cg

const supported = false

func clientRequest(*[6]uint64) uint64 { return 0 }
