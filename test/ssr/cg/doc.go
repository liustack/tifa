// Package cg lets a program tell callgrind where to count: it zeroes the counters and writes a
// dump on request, so one process can count each stage of its work apart. Outside valgrind every
// call does nothing. Only linux/amd64 has the requests, which valgrind reads from a sequence of
// instructions that is a no-op on a real CPU.
package cg

const (
	runningOnValgrind = 0x1001
	dumpStats         = 0x43540000
	zeroStats         = 0x43540001
)

// Running reports whether the program runs under valgrind.
func Running() bool { return request(runningOnValgrind) != 0 }

// Zero sets the counters of callgrind to 0.
func Zero() { request(zeroStats) }

// Dump writes the counters since the last Zero to the next dump file of callgrind.
func Dump() { request(dumpStats) }

func request(code uint64) uint64 {
	if !supported {
		return 0
	}
	return clientRequest(&[6]uint64{code})
}
