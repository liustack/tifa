package compare

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// BrowseStatus says, for one server of a run, what a run with these stamps does with the result at
// path: it keeps the results of the server (current), measures it again (stale, with why), refuses
// the whole file, or finds none (missing). It then says how many rounds of each part the file holds
// of it, out of how many: the closed rounds at the base sessions and whether its steps are done, the
// control for Tifa, and the latency rounds with their rate. dir is the
// folder of the traces. It reads and changes nothing else. A version of the server is not compared
// here: browse.sh puts the versions a server depends on in the stamp of its trace.
func BrowseStatus(path, dir, stamp string, stamps map[string]string, server string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("  result: missing: no %s\n", path), nil
	}
	if err != nil {
		return "", err
	}
	res, err := ReadBrowseResult(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	var b strings.Builder
	trace, _, _ := strings.Cut(server, "-") // the trace a spec replays (traceName)
	kept, known := res.ServerStamps[server]
	switch {
	case res.Stamp != stamp:
		b.WriteString("  result: stale, a run refuses the whole file: the stamp every server shares changed (the plans, the replayer, the toolchain or the settings), remove it to start again\n")
	case !known:
		b.WriteString("  result: missing: the result does not have it\n")
	case kept.Stamp != stamps[server]:
		b.WriteString("  result: stale, a run measures it again: its stamp changed (the trace it replays or the build it runs)\n")
	case kept.Trace != fileHash(TracePath(dir, trace)):
		b.WriteString("  result: stale, a run measures it again: its trace file changed\n")
	default:
		fmt.Fprintf(&b, "  result: current, measured %s\n", kept.Date)
	}
	rounds, _ := strconv.Atoi(res.Config["rounds"])
	sessions, _ := strconv.Atoi(res.Config["sessions"])
	of := func(n int) string {
		if n == 1 {
			return "1 round"
		}
		return fmt.Sprintf("%d rounds", n)
	}
	for _, sc := range res.Scenarios {
		want, done := closedRounds(server, rounds), 0
		for round := 1; round <= want; round++ {
			if res.closedDone(server, sc.Name, sessions, round) {
				done++
			}
		}
		steps := "steps not done"
		if t := res.Throughput[serverKey(sc.Name, server)]; t != nil {
			steps = fmt.Sprintf("steps done (%s at %d sessions)", t.Result, t.Sessions)
		}
		fmt.Fprintf(&b, "  throughput: %d of %s at %d sessions, %s\n", done, of(want), sessions, steps)
		if server == "tifa" {
			n := 0
			for _, x := range res.Control {
				if x.Scenario == sc.Name {
					n = 1
				}
			}
			fmt.Fprintf(&b, "  control: %d of 1 round\n", n)
		}
		// The one latency round is done with a valid try, or with the tries it gets.
		done, tries := 0, 0
		for _, x := range res.Latency {
			if x.Framework == server && x.Scenario == sc.Name {
				tries++
				if x.Valid {
					tries = maxTries
				}
			}
		}
		if tries >= maxTries {
			done = 1
		}
		at := "no rate yet"
		if rate := res.LatencyRate[sc.Name]; rate > 0 {
			at = fmt.Sprintf("at %.2f visits/s", rate)
		}
		fmt.Fprintf(&b, "  latency: %d of 1 round, %s\n", done, at)
	}
	return b.String(), nil
}

// BrowseCalibrationStatus says what base of the calibrations of the machine (CalibrationBase) the
// result of path has, "" when there is no result.
func BrowseCalibrationStatus(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	res, err := ReadBrowseResult(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	c := res.Calibration
	if c == nil {
		return "calibration: no base yet, a run sets it before its first round\n", nil
	}
	return fmt.Sprintf("calibration: base compute %.1f M/s, memory %.2f M/s, the medians of %d calibrations at %s\n", c.Compute, c.Memory, len(c.Runs), c.Date), nil
}
