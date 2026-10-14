//go:build unix && !darwin

package ssr

// maxrssUnit is the unit of Rusage.Maxrss in bytes.
const maxrssUnit = 1024
