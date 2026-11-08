package compare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
)

// The closed tables are throughput at the same number of users for every server, which is a
// server's capacity only where it used its cpus. The saturation phase finds, for each server that
// did not, the users at which it stops gaining: it steps the users up about 25% at a time and stops
// at the first step that gained less than the spread of the rounds, failed the rule, or used the
// cpus. What it found is kept in a checked-in file, and later runs play that count straight away.

// Saturated is the number of users at which a server saturated in a scenario, and how it was found.
type Saturated struct {
	Users int `json:"users"`
	// Why says why the probe stopped there.
	Why string `json:"why"`
	// Steps are the users the probe played above the users of every server.
	Steps []int `json:"steps,omitempty"`
	// Tifa is the commit and Date the date of the run that measured it, and ServerCPUs the cpus
	// the server had: a count measured on other cpus is not used.
	Tifa       string `json:"tifa"`
	Date       string `json:"date"`
	ServerCPUs int    `json:"serverCpus"`
	// Probed says that this run measured it, and did not read it from the file.
	Probed bool `json:"probed,omitempty"`
}

// SaturationFile is the checked-in file: per scenario and server, the users at which it saturated.
type SaturationFile map[string]map[string]Saturated

// maxProbeUsers is where a probe gives up stepping up.
const maxProbeUsers = 640

// stepUp is the next number of users of a probe: the next of 10, 12.5, 16, 20, 25, 32, 40, 50, 64,
// 80, 100 and so on, about 25% a step.
func stepUp(users int) int {
	for decade := 1.0; ; decade *= 10 {
		for _, m := range []float64{1, 1.25, 1.6, 2, 2.5, 3.2, 4, 5, 6.4, 8} {
			if n := int(math.Round(m * decade)); n > users {
				return n
			}
		}
	}
}

// needsMoreUsers says whether a server used under 0.95 of its cpus in its rounds (the median), so
// that the users did not reach its limit. It needs the cpus to be known and the CPU to be measured
// (from /proc, on Linux).
func needsMoreUsers(rounds []Round, cpus int) bool {
	if cpus == 0 || len(rounds) == 0 {
		return false
	}
	for _, x := range rounds {
		if x.CPUSeconds <= 0 {
			return false // not measured
		}
	}
	return cores(rounds) < 0.95*float64(cpus)
}

// cores is the median of the server cores a cell used.
func cores(rounds []Round) float64 {
	var v []float64
	for _, x := range rounds {
		if x.Seconds <= 0 {
			return 0
		}
		v = append(v, x.CPUSeconds/x.Seconds)
	}
	return Median(v)
}

// beats says whether more users gave more page views a second than the spread of the rounds: the
// slowest round with more users is faster than the fastest round with fewer.
func beats(more, fewer []Round) bool {
	if len(more) == 0 || len(fewer) == 0 {
		return false
	}
	return slices.Min(each(more, pageViewsPerSecond)) > slices.Max(each(fewer, pageViewsPerSecond))
}

// probe steps a server up from base users, whose rounds are at, and returns the users at which it
// saturated. run plays the rounds at a number of users. want is the rounds a cell needs.
func probe(base, cpus, want int, at []Round, run func(users int) ([]Round, error)) (Saturated, error) {
	if ok, why := verdict(at, want); !ok {
		return Saturated{Users: base, Why: fmt.Sprintf("failed at %d users: %s", base, why)}, nil
	}
	if !needsMoreUsers(at, cpus) {
		return Saturated{Users: base, Why: fmt.Sprintf("used %.2f of %d cpus at %d users", cores(at), cpus, base)}, nil
	}
	s := Saturated{Users: base}
	for users := stepUp(base); ; users = stepUp(users) {
		if users > maxProbeUsers {
			s.Why = fmt.Sprintf("still under 0.95 of %d cpus at %d users, where the probe stops", cpus, s.Users)
			return s, nil
		}
		rs, err := run(users)
		if err != nil {
			return s, err
		}
		s.Steps = append(s.Steps, users)
		if ok, why := verdict(rs, want); !ok {
			s.Why = fmt.Sprintf("failed at %d users: %s", users, why)
			return s, nil
		}
		if !beats(rs, at) {
			s.Why = fmt.Sprintf("at %d users the page views a second rose by less than the spread of the rounds", users)
			return s, nil
		}
		s.Users, at = users, rs
		if !needsMoreUsers(rs, cpus) {
			s.Why = fmt.Sprintf("used %.2f of %d cpus at %d users", cores(rs), cpus, users)
			return s, nil
		}
	}
}

// readSaturation reads the file, which is empty when it does not exist yet.
func readSaturation(path string) (SaturationFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return SaturationFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	f := SaturationFile{}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// writeSaturation sets one entry of the file and writes it again.
func writeSaturation(path, sc, fw string, s Saturated) error {
	f, err := readSaturation(path)
	if err != nil {
		return err
	}
	if f[sc] == nil {
		f[sc] = map[string]Saturated{}
	}
	s.Probed = false
	f[sc][fw] = s
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// saturation is the phase that plays, for each server of a scenario that did not use its cpus at
// the users of every server, the users at which it saturated: from the file, or found by a probe
// when the file has none for these cpus or when -probe asks for it, which then writes the file.
func (b *browse) saturation(ctx context.Context, sc BrowseScenario) error {
	if b.o.ServerCPUCount == 0 {
		b.logf("saturation %-10s skipped: the server is not pinned to cpus, so its cores are not known\n", sc.Name)
		return nil
	}
	file, err := readSaturation(b.o.SaturationFile)
	if err != nil {
		return err
	}
	if b.res.Saturated == nil {
		b.res.Saturated = map[string]*Saturated{}
	}
	for _, fw := range b.servers[sc.Name] {
		key := sustainKey(sc.Name, fw)
		if b.res.Saturated[key] != nil {
			continue
		}
		run := func(users int) ([]Round, error) { return b.saturationCell(ctx, fw, sc, users) }
		if s, ok := file[sc.Name][fw]; ok && !b.o.Probe && s.ServerCPUs == b.o.ServerCPUCount {
			b.logf("saturation %-10s %-12s %d users, measured on %s at %s\n", sc.Name, fw, s.Users, s.Tifa, s.Date)
			if s.Users > b.o.Users {
				if _, err := run(s.Users); err != nil {
					return err
				}
			}
			b.res.Saturated[key] = &s
			if err := b.commit(); err != nil {
				return err
			}
			continue
		}
		at := b.res.cellRounds(fw, sc.Name, b.o.Users)
		if len(at) == 0 {
			return fmt.Errorf("%s: the probe of %s needs its closed rounds at %d users: run the closed phase first", sc.Name, BrowseLabel(fw), b.o.Users)
		}
		s, err := probe(b.o.Users, b.o.ServerCPUCount, b.o.Rounds, at, run)
		if err != nil {
			return err
		}
		s.Tifa, s.Date, s.ServerCPUs, s.Probed = b.res.Version["tifa"], b.res.Date, b.o.ServerCPUCount, true
		b.logf("saturation %-10s %-12s %d users: %s\n", sc.Name, fw, s.Users, s.Why)
		b.res.Saturated[key] = &s
		if err := writeSaturation(b.o.SaturationFile, sc.Name, fw, s); err != nil {
			return err
		}
		if err := b.commit(); err != nil {
			return err
		}
	}
	return nil
}

// saturationCell plays the rounds of one server at a number of users that are not done yet, and
// returns the valid rounds of that cell.
func (b *browse) saturationCell(ctx context.Context, fw string, sc BrowseScenario, users int) ([]Round, error) {
	for round := 1; round <= b.o.Rounds; round++ {
		if b.res.saturationRounds(fw, sc.Name, users, round) > 0 {
			continue
		}
		r, err := b.closedRound(ctx, fw, sc, users, round, &b.res.Saturation)
		if err != nil {
			return nil, err
		}
		if r.TimedOut {
			break // a hung cell is reported as failed, as in the closed tables
		}
	}
	return b.res.saturationCell(fw, sc.Name, users), nil
}

// saturationRounds is how many valid rounds a cell above the users of every server has for a round.
func (r *BrowseResult) saturationRounds(fw, sc string, users, round int) int {
	n := 0
	for _, x := range r.Saturation {
		if x.Framework == fw && x.Scenario == sc && x.Users == users && x.Round == round && x.Valid {
			n++
		}
	}
	return n
}

// saturationCell is the valid rounds of a server at a number of users above those of every server.
func (r *BrowseResult) saturationCell(fw, sc string, users int) []Round {
	var out []Round
	for _, x := range r.Saturation {
		if x.Framework == fw && x.Scenario == sc && x.Users == users && x.Valid {
			out = append(out, x)
		}
	}
	return out
}
