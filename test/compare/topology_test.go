package compare

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseCPUList(t *testing.T) {
	for in, want := range map[string][]int{
		"0":       {0},
		"0,2":     {0, 2},
		"1-3":     {1, 2, 3},
		"0-1,4,6": {0, 1, 4, 6},
		" 2 ":     {2},
	} {
		got, err := ParseCPUList(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseCPUList(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a", "3-1", "1-", "-2", "1,,2"} {
		if got, err := ParseCPUList(bad); err == nil {
			t.Errorf("ParseCPUList(%q) = %v, want an error", bad, got)
		}
	}
}

// A machine with two physical cores and two threads each: cpus 0 and 2 are one core, 1 and 3 the
// other. cpu 4 is on a second package and has the core id 0 as well.
func topologyFS() fstest.MapFS {
	f := fstest.MapFS{}
	add := func(cpu, pkg, core string) {
		f["cpu"+cpu+"/topology/physical_package_id"] = &fstest.MapFile{Data: []byte(pkg + "\n")}
		f["cpu"+cpu+"/topology/core_id"] = &fstest.MapFile{Data: []byte(core + "\n")}
	}
	add("0", "0", "0")
	add("1", "0", "1")
	add("2", "0", "0")
	add("3", "0", "1")
	add("4", "1", "0")
	f["cpufreq"] = &fstest.MapFile{Data: []byte("not a cpu")}
	f["cpu"] = &fstest.MapFile{Data: []byte("not a cpu either")}
	return f
}

func TestReadTopology(t *testing.T) {
	topo, err := ReadTopology(topologyFS())
	if err != nil {
		t.Fatal(err)
	}
	if len(topo) != 5 {
		t.Fatalf("read %d cpus, want 5: %v", len(topo), topo)
	}
	if topo[0] != topo[2] || topo[0] == topo[1] || topo[0] == topo[4] {
		t.Errorf("wrong cores: %v", topo)
	}
	bad := fstest.MapFS{"cpu0/topology/core_id": &fstest.MapFile{Data: []byte("x")}, "cpu0/topology/physical_package_id": &fstest.MapFile{Data: []byte("0")}}
	if _, err := ReadTopology(bad); err == nil {
		t.Error("a core id that is not a number was accepted")
	}
	if _, err := ReadTopology(fstest.MapFS{}); err == nil {
		t.Error("an empty tree was accepted")
	}
}

func TestCheckPlacement(t *testing.T) {
	topo, _ := ReadTopology(topologyFS())
	cases := []struct {
		server, load []int
		bad          string // "" when fine, else a part of the error
	}{
		{[]int{0}, []int{1}, ""},
		{[]int{0}, []int{4}, ""}, // same core id on another package is another core
		{[]int{0}, []int{2}, "cpu 0 and cpu 2"},
		{[]int{0, 1}, []int{3}, "cpu 1 and cpu 3"},
		{[]int{0}, []int{0}, "cpu 0 and cpu 0"},
		{[]int{0}, []int{9}, "cpu 9"},
	}
	for _, c := range cases {
		err := CheckPlacement(topo, c.server, c.load)
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("server %v load %v: %v", c.server, c.load, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("server %v load %v: error %v, want one with %q", c.server, c.load, err, c.bad)
		}
	}
}

func TestPlacementFor(t *testing.T) {
	p, err := PlacementFor("darwin", nil, "", "")
	if err != nil || p.Checked || !strings.Contains(p.Note, "not checked") {
		t.Errorf("darwin without pinning: %+v, %v", p, err)
	}
	if _, err := PlacementFor("darwin", nil, "0", "1"); err == nil {
		t.Error("pinning on darwin was accepted")
	}
	p, err = PlacementFor("linux", topologyFS(), "", "")
	if err != nil || p.Checked || !strings.Contains(p.Note, "not checked") {
		t.Errorf("linux without pinning: %+v, %v", p, err)
	}
	p, err = PlacementFor("linux", topologyFS(), "0", "1")
	if err != nil || !p.Checked || p.Server != "0" || p.Load != "1" {
		t.Errorf("linux, separate cores: %+v, %v", p, err)
	}
	if _, err := PlacementFor("linux", topologyFS(), "0", "2"); err == nil || !strings.Contains(err.Error(), "same physical core") {
		t.Errorf("linux, one core: error %v", err)
	}
	if _, err := PlacementFor("linux", topologyFS(), "0", ""); err == nil {
		t.Error("only one side given was accepted")
	}
}
