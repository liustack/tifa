package compare

import (
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// Core names one physical core: a package and a core id in it. The threads of a core share it.
type Core struct{ Package, ID int }

// ParseCPUList reads a list of cpus as taskset -c takes it: "0", "0,2" or "0-3,6".
func ParseCPUList(s string) ([]int, error) {
	var cpus []int
	for part := range strings.SplitSeq(strings.TrimSpace(s), ",") {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
		from, err := strconv.Atoi(lo)
		if err != nil || from < 0 {
			return nil, fmt.Errorf("%q is not a list of cpus", s)
		}
		to := from
		if isRange {
			if to, err = strconv.Atoi(hi); err != nil || to < from {
				return nil, fmt.Errorf("%q is not a list of cpus", s)
			}
		}
		for c := from; c <= to; c++ {
			cpus = append(cpus, c)
		}
	}
	return cpus, nil
}

// ReadTopology reads the physical core of every cpu from a tree laid out as
// /sys/devices/system/cpu: cpuN/topology/physical_package_id and cpuN/topology/core_id.
func ReadTopology(fsys fs.FS) (map[int]Core, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	topo := map[int]Core{}
	for _, e := range entries {
		n, ok := strings.CutPrefix(e.Name(), "cpu")
		cpu, err := strconv.Atoi(n)
		if !ok || err != nil {
			continue // cpufreq, cpuidle, ...
		}
		var core Core
		for name, dst := range map[string]*int{"physical_package_id": &core.Package, "core_id": &core.ID} {
			path := e.Name() + "/topology/" + name
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return nil, err
			}
			if *dst, err = strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
				return nil, fmt.Errorf("%s holds %q, not a number", path, strings.TrimSpace(string(data)))
			}
		}
		topo[cpu] = core
	}
	if len(topo) == 0 {
		return nil, fmt.Errorf("no cpu with a topology found")
	}
	return topo, nil
}

// CheckPlacement fails when a cpu of the server and a cpu of the load are the same cpu or threads
// of one physical core, which would let the load tool take time from the server.
func CheckPlacement(topo map[int]Core, server, load []int) error {
	for _, s := range server {
		cs, ok := topo[s]
		if !ok {
			return fmt.Errorf("cpu %d is not in the topology of this machine", s)
		}
		for _, l := range load {
			cl, ok := topo[l]
			if !ok {
				return fmt.Errorf("cpu %d is not in the topology of this machine", l)
			}
			if cs == cl {
				return fmt.Errorf("cpu %d and cpu %d are on the same physical core (package %d, core %d): put the server and the load on different cores", s, l, cs.Package, cs.ID)
			}
		}
	}
	return nil
}

// Placement says where the server and the load ran and whether that was checked.
type Placement struct {
	Server  string `json:"serverCpus,omitempty"`
	Load    string `json:"loadCpus,omitempty"`
	Checked bool   `json:"checked"`
	Note    string `json:"note"`
}

// PlacementFor checks the cpus for the server and the load on the given OS. topology is the tree
// of /sys/devices/system/cpu on Linux. Without cpus, or off Linux where there is no pinning,
// nothing is checked and the Placement says so.
func PlacementFor(goos string, topology fs.FS, server, load string) (Placement, error) {
	p := Placement{Server: server, Load: load}
	if server == "" && load == "" {
		p.Note = "not checked: the server and the load are not pinned to cpus"
		return p, nil
	}
	if goos != "linux" {
		return p, fmt.Errorf("pinning to cpus needs taskset, which is on Linux only: leave the cpus empty on %s", goos)
	}
	if server == "" || load == "" {
		return p, fmt.Errorf("give both -server-cpus and -load-cpus, or neither")
	}
	s, err := ParseCPUList(server)
	if err != nil {
		return p, err
	}
	l, err := ParseCPUList(load)
	if err != nil {
		return p, err
	}
	topo, err := ReadTopology(topology)
	if err != nil {
		return p, fmt.Errorf("cannot check the placement: %w", err)
	}
	if err := CheckPlacement(topo, s, l); err != nil {
		return p, err
	}
	p.Checked = true
	p.Note = "server and load are on different physical cores"
	return p, nil
}

// SysTopology is the topology of this machine, on Linux.
func SysTopology() fs.FS { return os.DirFS("/sys/devices/system/cpu") }
