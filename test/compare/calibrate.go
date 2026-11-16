package compare

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"
)

// The calibration is a fixed program that says how fast the machine is now, apart from Tifa, its
// engine, the shop and the Node servers, so that its numbers mean the same for months: a host can
// change the speed of a virtual machine from one minute to the next, and nothing in a round shows it.
// It times two kinds of work, each a fixed amount, wall clock:
//
//   - compute: a loop of integer arithmetic over a table of 4 KB, which stays in the L1 cache and
//     follows the clock of the core.
//   - memory: a walk through a ring of cache lines in random order over 128 MB, far beyond the L3 of
//     a core complex (32 MB on an EPYC 7B13), each step a miss: it follows what neighbours do to the
//     caches and the memory bus.
//
// Each is repeated, and its speed is the median of the repeats, in millions of steps a second. The
// amounts are set so that a calibration takes about 5 seconds on the machines of the comparison.
// Changing them, or the work, changes every speed: a result keeps the base it was measured against.

// calibrationWork is the amount of work of a calibration.
type calibrationWork struct {
	Compute int // iterations of the compute loop a repeat
	Steps   int // steps of the memory walk a repeat
	Lines   int // cache lines of the ring of the walk
	Repeats int
}

// calibrationDefault is the work of `compare calibrate`.
var calibrationDefault = calibrationWork{Compute: 300_000_000, Steps: 6_000_000, Lines: 128 << 20 / 64, Repeats: 3}

// Calibration is what a calibration measured: the two speeds, in millions of steps a second, the
// medians of their repeats.
type Calibration struct {
	Compute     float64   `json:"compute"`
	Memory      float64   `json:"memory"`
	ComputeRuns []float64 `json:"computeRuns"`
	MemoryRuns  []float64 `json:"memoryRuns"`
}

// String is the line `compare calibrate` prints.
func (c Calibration) String() string {
	return fmt.Sprintf("compute %.1f M/s (repeats %s), memory %.2f M/s (repeats %s)", c.Compute, fmtRuns(c.ComputeRuns, "%.1f"), c.Memory, fmtRuns(c.MemoryRuns, "%.2f"))
}

// calibrationSink keeps the results of the work, so that the compiler cannot drop it.
var calibrationSink uint64

// Calibrate runs the calibration of `compare calibrate`.
func Calibrate() Calibration { return runCalibration(calibrationDefault) }

// CalibrationJSON is a calibration as `compare calibrate -json` prints it.
func CalibrationJSON(c Calibration) ([]byte, error) { return json.Marshal(c) }

func runCalibration(w calibrationWork) Calibration {
	ring := newWalkRing(w.Lines)
	ring.walk(min(w.Steps, w.Lines)) // the pages of the ring are in memory and in the TLB as in every repeat
	var c Calibration
	for range w.Repeats {
		start := time.Now()
		calibrationSink += computeWork(w.Compute)
		c.ComputeRuns = append(c.ComputeRuns, float64(w.Compute)/time.Since(start).Seconds()/1e6)
		start = time.Now()
		calibrationSink += uint64(ring.walk(w.Steps))
		c.MemoryRuns = append(c.MemoryRuns, float64(w.Steps)/time.Since(start).Seconds()/1e6)
	}
	c.Compute, c.Memory = Median(c.ComputeRuns), Median(c.MemoryRuns)
	return c
}

// computeWork is n iterations of a loop of integer arithmetic, loads and stores over a table of 4 KB.
func computeWork(n int) uint64 {
	var table [512]uint64
	for i := range table {
		table[i] = uint64(i) * 0x9E3779B97F4A7C15
	}
	a, b := uint64(1), uint64(2)
	for range n {
		a ^= a << 13
		a ^= a >> 7
		a ^= a << 17
		b = b*6364136223846793005 + table[a&511]
		table[b&511] ^= a
	}
	return a ^ b
}

// lineWords is how many uint32 a cache line of 64 bytes holds: the ring uses the first of each line.
const lineWords = 16

// walkRing is a ring through every cache line of a buffer, in an order drawn once from a fixed seed.
type walkRing struct {
	buf   []uint32
	lines int
}

// newWalkRing makes a ring of lines cache lines: one cycle through all of them, each line linked to the next of an order shuffled from a fixed seed.
func newWalkRing(lines int) *walkRing {
	r := &walkRing{buf: make([]uint32, lines*lineWords), lines: lines}
	order := make([]uint32, lines)
	for i := range order {
		order[i] = uint32(i)
	}
	x := uint64(0x2545F4914F6CDD1D)
	for i := lines - 1; i > 0; i-- {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		j := int(x % uint64(i))
		order[i], order[j] = order[j], order[i]
	}
	for i := range order {
		from, to := order[i], order[(i+1)%lines]
		r.buf[int(from)*lineWords] = to
	}
	return r
}

// next is the line after line at.
func (r *walkRing) next(at uint32) uint32 { return r.buf[int(at)*lineWords] }

// walk takes steps steps of the ring from line 0 and returns the line it ends on.
func (r *walkRing) walk(steps int) uint32 {
	at := uint32(0)
	for range steps {
		at = r.buf[int(at)*lineWords]
	}
	return at
}

// fmtRuns writes the speeds of the repeats one after the other.
func fmtRuns(runs []float64, format string) string {
	var out []string
	for _, v := range runs {
		out = append(out, fmt.Sprintf(format, v))
	}
	return strings.Join(out, " ")
}
