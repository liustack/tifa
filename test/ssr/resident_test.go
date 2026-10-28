package ssr_test

// What one VM of the shop costs the process, in memory the system counts. Build the shop first (see
// shop_test.go), then run:
//
//	TIFA_SSR_SHOP=1 go test -run ShopVMResident -v ./test/ssr
//
// TestShopVMResident starts this test binary twice as TestShopVMResidentChild, once with a pool of 1 VM and
// once with TIFA_SSR_VMS (8), and takes the memory of one VM from the difference of the two processes,
// (RSS of N VMs - RSS of 1 VM) / (N - 1). Two processes, because after the first pool the Go heap and the
// allocator keep pages that a second pool in the same process would count as its own. Each child prints
// the VmRSS of its process after loading, after rendering every page with every VM, and idle, and the
// highest VmRSS it reached (VmHWM) in a line "vmhwm-kb: N", which `sjdev bench run` reads. The child of 1
// VM also prints what the engine counts for one VM, rendered the way the pool renders (a young GC after
// each render, a full one at the end).

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/ssr"
)

// procKiB returns a field of /proc/self/status in KiB, such as VmRSS or VmHWM.
func procKiB(t testing.TB, field string) int {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Skip("memory is read from /proc/self/status, which this system does not have")
	}
	n, err := parseProcKiB(string(b), field)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// parseProcKiB reads a field of the text of /proc/PID/status.
func parseProcKiB(status, field string) (int, error) {
	for _, line := range strings.Split(status, "\n") {
		rest, ok := strings.CutPrefix(line, field+":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			return 0, fmt.Errorf("unexpected %s line %q", field, line)
		}
		return strconv.Atoi(f[0])
	}
	return 0, fmt.Errorf("no %s in the status", field)
}

// residentKiB is the VmRSS after the Go heap went back to the system.
func residentKiB(t testing.TB) int {
	t.Helper()
	runtime.GC()
	debug.FreeOSMemory()
	return procKiB(t, "VmRSS")
}

func TestParseProcKiB(t *testing.T) {
	status := "Name:\tx\nVmHWM:\t  2048 kB\nVmRSS:\t 1024 kB\n"
	if n, err := parseProcKiB(status, "VmRSS"); err != nil || n != 1024 {
		t.Errorf("VmRSS = %d, %v", n, err)
	}
	if n, err := parseProcKiB(status, "VmHWM"); err != nil || n != 2048 {
		t.Errorf("VmHWM = %d, %v", n, err)
	}
	if _, err := parseProcKiB(status, "VmSwap"); err == nil {
		t.Error("a missing field parsed")
	}
	if _, err := parseProcKiB("VmRSS:\t5 MB\n", "VmRSS"); err == nil {
		t.Error("a unit other than kB parsed")
	}
}

var residentLine = regexp.MustCompile(`resident: vms (\d+) start (\d+) loaded (\d+) rendered (\d+) idle (\d+)`)

// residentSample is the line a child prints.
type residentSample struct{ vms, start, loaded, rendered, idle int }

func parseResident(out string) (residentSample, error) {
	m := residentLine.FindStringSubmatch(out)
	if m == nil {
		return residentSample{}, fmt.Errorf("no resident line in %q", out)
	}
	var v [5]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return residentSample{v[0], v[1], v[2], v[3], v[4]}, nil
}

// perVM returns the KiB one VM takes: the difference of the processes over the difference of the VMs.
func perVM(one, many residentSample) (loaded, rendered float64, err error) {
	if many.vms <= one.vms {
		return 0, 0, fmt.Errorf("%d VMs against %d: the second process must have more", many.vms, one.vms)
	}
	d := float64(many.vms - one.vms)
	return float64(many.loaded-one.loaded) / d, float64(many.rendered-one.rendered) / d, nil
}

func TestPerVM(t *testing.T) {
	one := residentSample{vms: 1, loaded: 30000, rendered: 40000}
	many := residentSample{vms: 8, loaded: 30000 + 7*8000, rendered: 40000 + 7*12000}
	l, r, err := perVM(one, many)
	if err != nil || l != 8000 || r != 12000 {
		t.Errorf("perVM = %v, %v, %v", l, r, err)
	}
	if _, _, err := perVM(many, one); err == nil {
		t.Error("fewer VMs in the second process accepted")
	}
}

func TestShopVMResident(t *testing.T) {
	if os.Getenv("TIFA_SSR_SHOP") == "" {
		t.Skip("set TIFA_SSR_SHOP=1 after running tifa build in examples/shop")
	}
	many := envInt("TIFA_SSR_VMS", 8)
	if many < 2 {
		t.Fatal("TIFA_SSR_VMS must be at least 2")
	}
	run := func(vms int) residentSample {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestShopVMResidentChild$", "-test.v")
		cmd.Env = append(os.Environ(), "TIFA_SSR_RESIDENT_CHILD=1", "TIFA_SSR_VMS_CHILD="+strconv.Itoa(vms))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child with %d VMs: %v\n%s", vms, err, out)
		}
		s, err := parseResident(string(out))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("child with %d VMs:\n%s", vms, out)
		return s
	}
	one, n := run(1), run(many)
	loaded, rendered, err := perVM(one, n)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("one VM, from %d against 1 (MiB): after loading %.1f, after rendering %.1f", many, loaded/1024, rendered/1024)
	fmt.Printf("per-vm-mib: loaded %.1f rendered %.1f\n", loaded/1024, rendered/1024)
}

// TestShopVMResidentChild is the process TestShopVMResident starts. It does nothing on its own.
func TestShopVMResidentChild(t *testing.T) {
	if os.Getenv("TIFA_SSR_RESIDENT_CHILD") == "" {
		t.Skip("started by TestShopVMResident")
	}
	source, pages := shopBundle(t)
	n := envInt("TIFA_SSR_VMS_CHILD", 1)
	rounds := envInt("TIFA_SSR_ROUNDS", 32)
	cfg := shopConfig(n)
	start := residentKiB(t)
	p, err := ssr.NewPool(source, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	loaded := residentKiB(t)
	if err := renderParallel(p, pages, n, rounds*n*len(pages)); err != nil {
		t.Fatal(err)
	}
	rendered := residentKiB(t)
	time.Sleep(500 * time.Millisecond) // the collections after the last renders
	idle := residentKiB(t)
	fmt.Printf("resident: vms %d start %d loaded %d rendered %d idle %d\n", n, start, loaded, rendered, idle)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if n == 1 {
		engineMemory(t, source, pages, rounds)
	}
	fmt.Printf("vmhwm-kb: %d\n", procKiB(t, "VmHWM"))
}

// engineMemory renders one VM the way the pool does and prints what the engine counts after each step.
func engineMemory(t *testing.T, source string, pages []shopPage, rounds int) {
	cfg := shopConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	report := func(when string) {
		u := ssr.VMMemory(vm.VM, false)
		fmt.Printf("engine %s: malloc %d footprint %d chunks %d release-candidate %d\n", when, u.MallocSize, u.Footprint, u.Chunks, u.ReleaseCandidate)
	}
	report("loaded")
	for i := range rounds * len(pages) {
		page := pages[i%len(pages)]
		html, err := ssr.RenderVM(vm, renderInput(t, page.input), page.input.Texts, cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
		vm.RunYoungGC()
	}
	report("rendered")
	vm.RunGC()
	report("full GC")
	released, err := vm.ReleaseMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("engine released %d\n", released)
	report("released")
}

// envInt returns the integer in the environment variable name, or def when it is not set.
func envInt(name string, def int) int {
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("%s=%q is not a number", name, s))
	}
	return n
}
