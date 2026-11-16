//go:build linux && amd64

package cg

const supported = true

// clientRequest is in cg_amd64.s.
//
//go:noescape
func clientRequest(args *[6]uint64) uint64
