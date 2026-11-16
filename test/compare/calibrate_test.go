package compare

import (
	"testing"
)

// The work of a calibration is fixed: the same loop and the same walk give the same checksum on every
// run and every machine, so that a speed is work done a second and nothing else.
func TestTheCalibrationWorkIsTheSameEveryTime(t *testing.T) {
	if a, b := computeWork(1_000_000), computeWork(1_000_000); a != b || a == 0 {
		t.Errorf("the compute loop gave %d and %d", a, b)
	}
	ring := newWalkRing(1 << 12)
	if a, b := ring.walk(100_000), ring.walk(100_000); a != b {
		t.Errorf("the walk gave %d and %d", a, b)
	}
	// The ring is one cycle through every line: a walk of as many steps as lines comes back to the start
	// having seen each line once.
	seen := map[uint32]bool{}
	at := uint32(0)
	for range ring.lines {
		if seen[at] {
			t.Fatalf("line %d seen twice before the cycle ended", at)
		}
		seen[at] = true
		at = ring.next(at)
	}
	if at != 0 || len(seen) != ring.lines {
		t.Errorf("the walk saw %d of %d lines and ended at %d", len(seen), ring.lines, at)
	}
}

// A calibration is the median of its repeats of each kind of work, as work done a second.
func TestACalibrationIsTheMedianOfItsRepeats(t *testing.T) {
	c := runCalibration(calibrationWork{Compute: 200_000, Steps: 20_000, Lines: 1 << 12, Repeats: 3})
	if len(c.ComputeRuns) != 3 || len(c.MemoryRuns) != 3 {
		t.Fatalf("%d and %d repeats, want 3 of each", len(c.ComputeRuns), len(c.MemoryRuns))
	}
	if c.Compute != Median(c.ComputeRuns) || c.Memory != Median(c.MemoryRuns) || c.Compute <= 0 || c.Memory <= 0 {
		t.Errorf("the calibration %+v", c)
	}
}
