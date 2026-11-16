package compare

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// A host can change the speed of a virtual machine from one minute to the next, and nothing in a
// round shows it: rows measured at different minutes are then not comparable. So every round of
// `compare browse` is timed against the speed of the machine at the start of the run, by the fixed
// program of `compare calibrate` (Calibrate), run on the cpus of the server inside the hold of the
// round:
//
//   - before the warm-up of a round (before its server starts): off the base, the run gives the hold
//     back, waits calibrationWait and probes again, up to calibrationProbes times, then stops.
//   - right before the clock and right after the round: either off the base, the round does not
//     count (the reason "machine") and is played again on a fresh server, without using up a try of
//     the server, up to machineReplays times, then the run stops.
//
// The base is the median of calibrationBaseRuns calibrations at the start of a run that has none,
// kept in the result like the prepared visits: a run that continues uses the base it finds.

// calibrationTolerance is how far a speed of the calibration may be from the base, as a share, either
// way, before the machine is taken to be off its speed.
var calibrationTolerance = 0.03

// calibrationGated are the speeds the gates judge. The others are kept and shown, not judged.
var calibrationGated = []string{"compute"}

// calibrationWait is how long the gate before the warm-up waits, with the hold given back, before it
// probes again. Tests shorten it.
var calibrationWait = 2 * time.Minute

const (
	calibrationProbes   = 15 // probes of the gate before the warm-up of a round
	machineReplays      = 5  // rounds of a cell played again because the machine was off its speed
	calibrationBaseRuns = 5  // calibrations the base is the median of
)

// The points of a round at which the machine is calibrated.
const (
	calibrationBeforeWarmUp = "before the warm-up"
	calibrationBeforeClock  = "before the clock"
	calibrationAfterRound   = "after the round"
)

// CalibrationBase is the speed of the machine at the start of a run: the median of each speed over
// calibrationBaseRuns calibrations, with them.
type CalibrationBase struct {
	Compute float64 `json:"compute"`
	Memory  float64 `json:"memory"`
	// Tolerance and Gated are calibrationTolerance and calibrationGated of the run, which judged its rounds.
	Tolerance float64       `json:"tolerance"`
	Gated     []string      `json:"gated"`
	Runs      []Calibration `json:"runs"`
	Date      string        `json:"date"`
}

// RoundCalibration is a calibration of a round: when it ran, its speeds and their shares of the base.
type RoundCalibration struct {
	When         string  `json:"when"`
	Compute      float64 `json:"compute"`
	Memory       float64 `json:"memory"`
	ComputeShare float64 `json:"computeShare"`
	MemoryShare  float64 `json:"memoryShare"`
}

// errMachineSpeed is the error of a run that stops because the machine is not at its speed.
var errMachineSpeed = errors.New("the machine is not at the speed of this run")

// machineStop is the error that stops a run whose machine stayed off the speed of its base.
func machineStop(what string) error {
	return fmt.Errorf("%w: %s. The run continues from here when it is started again", errMachineSpeed, what)
}

// CalibrateChild returns the calibration of a run of `compare browse`: this program run as `compare
// calibrate -json`, pinned to the cpus of the server on Linux (taskset), and not pinned elsewhere.
func CalibrateChild(serverCPUs string) func(ctx context.Context) (Calibration, error) {
	return func(ctx context.Context) (Calibration, error) {
		self, err := os.Executable()
		if err != nil {
			return Calibration{}, err
		}
		args := []string{self, "calibrate", "-json"}
		if runtime.GOOS == "linux" && serverCPUs != "" {
			args = append([]string{"taskset", "-c", serverCPUs}, args...)
		}
		out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
		if err != nil {
			return Calibration{}, fmt.Errorf("calibrate: %w", err)
		}
		var c Calibration
		if err := json.Unmarshal(out, &c); err != nil {
			return Calibration{}, fmt.Errorf("calibrate: %w", err)
		}
		return c, nil
	}
}

// share is a calibration against the base, at a point of a round.
func (base *CalibrationBase) share(when string, c Calibration) RoundCalibration {
	return RoundCalibration{When: when, Compute: c.Compute, Memory: c.Memory, ComputeShare: c.Compute / base.Compute, MemoryShare: c.Memory / base.Memory}
}

// offBase says how a calibration of a round is off the base, or "" when every gated speed is within
// calibrationTolerance of it.
func offBase(c RoundCalibration) string {
	var off []string
	for _, speed := range calibrationGated {
		s := map[string]float64{"compute": c.ComputeShare, "memory": c.MemoryShare}[speed]
		if math.Abs(s-1) > calibrationTolerance+1e-9 { // 103 of 100 is within 3%, though not in floating point
			off = append(off, fmt.Sprintf("%s at %.1f%% of the base", speed, 100*s))
		}
	}
	if len(off) == 0 {
		return ""
	}
	return fmt.Sprintf("%s, %s (more than %.0f%% off)", c.When, strings.Join(off, " and "), 100*calibrationTolerance)
}

// calibrating says whether the run calibrates the machine (BrowseOptions.Calibrate).
func (b *browse) calibrating() bool { return b.o.Calibrate != nil }

// probe calibrates the machine at a point of a round, against the base.
func (b *browse) probe(ctx context.Context, when string) (RoundCalibration, error) {
	c, err := b.o.Calibrate(ctx)
	if err != nil {
		return RoundCalibration{}, err
	}
	rc := b.res.Calibration.share(when, c)
	b.logf("calibrate %-18s compute %.1f M/s (%.1f%%), memory %.2f M/s (%.1f%%)\n", when, rc.Compute, 100*rc.ComputeShare, rc.Memory, 100*rc.MemoryShare)
	return rc, nil
}

// calibrationBase sets the base of a run that has none: the median of calibrationBaseRuns calibrations,
// inside one hold. A run that continues a result keeps the base it finds.
func (b *browse) calibrationBase(ctx context.Context) error {
	if !b.calibrating() || b.res.Calibration != nil {
		return nil
	}
	unlock, err := b.takeHold(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	base := &CalibrationBase{Tolerance: calibrationTolerance, Gated: calibrationGated, Date: time.Now().UTC().Format("2006-01-02 15:04:05 UTC")}
	for range calibrationBaseRuns {
		c, err := b.o.Calibrate(ctx)
		if err != nil {
			return err
		}
		base.Runs = append(base.Runs, c)
	}
	var compute, memory []float64
	for _, c := range base.Runs {
		compute, memory = append(compute, c.Compute), append(memory, c.Memory)
	}
	base.Compute, base.Memory = Median(compute), Median(memory)
	b.res.Calibration = base
	b.logf("calibrate the base of the run: compute %.1f M/s, memory %.2f M/s, the medians of %d calibrations\n", base.Compute, base.Memory, calibrationBaseRuns)
	return b.commit()
}

// gateBeforeWarmUp is the gate before the warm-up of a round, inside the hold that unlock gives back:
// it returns the calibration of the round there and the hold to keep, once the machine is at the speed
// of the base. Off the base it gives the hold back, waits calibrationWait, takes the hold again and
// probes again, up to calibrationProbes probes, then stops the run.
func (b *browse) gateBeforeWarmUp(ctx context.Context, unlock func()) (*RoundCalibration, func(), error) {
	for probe := 1; ; probe++ {
		rc, err := b.probe(ctx, calibrationBeforeWarmUp)
		if err != nil {
			unlock()
			return nil, nil, err
		}
		off := offBase(rc)
		if off == "" {
			return &rc, unlock, nil
		}
		unlock()
		if probe >= calibrationProbes {
			return nil, nil, machineStop(fmt.Sprintf("%d probes before the warm-up of a round, %s apart, were off its base, the last %s", probe, calibrationWait, off))
		}
		b.logf("calibrate off the base %s: the hold is given back for %s (probe %d of %d)\n", off, calibrationWait, probe, calibrationProbes)
		if err := sleep(ctx, calibrationWait); err != nil {
			return nil, nil, err
		}
		if unlock, err = b.takeHold(ctx); err != nil {
			return nil, nil, err
		}
	}
}

// machineOff says how the calibrations of a played round are off the base, before its clock or after
// it, or "" when they are not.
func machineOff(rs []RoundCalibration) string {
	for _, c := range rs {
		if c.When == calibrationBeforeWarmUp {
			continue
		}
		if off := offBase(c); off != "" {
			return off
		}
	}
	return ""
}

// replayWhenOff plays a round with play, and plays it again while a calibration of the round before its
// clock or after it is off the base: such a round does not count, whatever the server did, and uses
// up no try of the server. The reasons are kept in the round that counts (MachineReplays). After
// machineReplays rounds played again the run stops.
func (b *browse) replayWhenOff(play func() (*Round, error)) (*Round, error) {
	var why []string
	for {
		out, err := play()
		if err != nil || out == nil {
			return out, err
		}
		off := machineOff(out.Calibrations)
		if off == "" {
			out.MachineReplays = why
			return out, nil
		}
		why = append(why, off)
		b.logf("machine   %s %s: the round does not count and is played again (%d of %d)\n", out.Scenario, out.Framework, len(why), machineReplays)
		if len(why) >= machineReplays {
			return nil, machineStop(fmt.Sprintf("%d rounds of %s in a row were off its base, the last %s", len(why), BrowseLabel(out.Framework), off))
		}
	}
}

// calibrationShares lists the shares of the base of a round's calibrations, in their order.
func calibrationShares(rs []RoundCalibration, speed string) []float64 {
	var out []float64
	for _, c := range rs {
		out = append(out, map[string]float64{"compute": c.ComputeShare, "memory": c.MemoryShare}[speed])
	}
	return out
}

// onOff writes a setting that is on or off.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
