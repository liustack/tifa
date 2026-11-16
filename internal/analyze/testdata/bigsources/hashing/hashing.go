// Package hashing has a function named Sum64 of the kind fnv, maphash and xxhash have.
package hashing

func Sum64(s string) uint64 { return uint64(len(s)) << 60 }

func Sum64String(s string) uint64 { return Sum64(s) }
