package main

// tifa deploy status --all looks at every server in ~/.tifa/config.yaml once and prints what it
// finds. It is a look, not a monitor: nothing refreshes, alerts or runs in the background.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// statusAllParallel is how many servers --all asks at the same time.
const statusAllParallel = 4

// statusServerTimeout is how long one server may take, with all its apps.
const statusServerTimeout = 90 * time.Second

// The files the facts script reads. Tests point them at files they write.
var (
	factsOSRelease = "/etc/os-release"
	factsMeminfo   = "/proc/meminfo"
	factsUptime    = "/proc/uptime"
	factsLoadavg   = "/proc/loadavg"
)

// factsBody runs as the deploy user and prints facts about the server, one key=value per line,
// and the apps of the user. Every line is optional: a server that lacks a file or a tool leaves
// the line out, and the script never fails.
const factsBody = `if [ -r "$osrelease_file" ]; then
	( . "$osrelease_file"; if [ -n "${PRETTY_NAME:-}" ]; then echo "os=$PRETTY_NAME"; fi )
fi
echo "arch=$(uname -m)"
n=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || true)
if [ -n "$n" ]; then echo "cpus=$n"; fi
if [ -r "$meminfo_file" ]; then
	awk '/^MemTotal:/ { print "mem_total_kb=" $2 } /^MemAvailable:/ { print "mem_available_kb=" $2 }' "$meminfo_file"
fi
df -Pk "$HOME" 2>/dev/null | awk 'NR == 2 { print "disk_total_kb=" $2; print "disk_used_kb=" $3 }'
if [ -r "$uptime_file" ]; then awk '{ print "uptime_s=" int($1) }' "$uptime_file"; fi
if [ -r "$loadavg_file" ]; then awk '{ print "load=" $1 " " $2 " " $3 }' "$loadavg_file"; fi
echo "machine=$(hostname -s)-$(head -c 8 /etc/machine-id 2>/dev/null)"
for f in "$HOME"/*/port; do
	if [ -f "$f" ]; then echo "app=$(basename "$(dirname "$f")")"; fi
done
exit 0
`

func factsScript() string {
	return script(factsBody, "osrelease_file", factsOSRelease, "meminfo_file", factsMeminfo,
		"uptime_file", factsUptime, "loadavg_file", factsLoadavg)
}

// serverFacts is what the facts script says about a server. A number the server did not say is 0.
type serverFacts struct {
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	CPUs           int      `json:"cpus"`
	MemoryTotalKiB int64    `json:"memoryTotalKiB"`
	MemoryAvailKiB int64    `json:"memoryAvailableKiB"`
	DiskTotalKiB   int64    `json:"diskTotalKiB"`
	DiskUsedKiB    int64    `json:"diskUsedKiB"`
	UptimeSeconds  int64    `json:"uptimeSeconds"`
	Load1          float64  `json:"load1"`
	Load5          float64  `json:"load5"`
	Load15         float64  `json:"load15"`
	Machine        string   `json:"-"`
	Apps           []string `json:"-"`
}

// parseServerFacts reads the output of factsScript.
func parseServerFacts(out string) (serverFacts, error) {
	var f serverFacts
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return f, fmt.Errorf("the server printed a line tifa cannot read: %q", line)
		}
		value = strings.TrimSpace(value)
		num := func(dst *int64) error {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return fmt.Errorf("the server gave %s as %q", key, value)
			}
			*dst = n
			return nil
		}
		var err error
		switch key {
		case "os":
			f.OS = value
		case "arch":
			f.Arch = value
		case "cpus":
			var n int64
			err = num(&n)
			f.CPUs = int(n)
		case "mem_total_kb":
			err = num(&f.MemoryTotalKiB)
		case "mem_available_kb":
			err = num(&f.MemoryAvailKiB)
		case "disk_total_kb":
			err = num(&f.DiskTotalKiB)
		case "disk_used_kb":
			err = num(&f.DiskUsedKiB)
		case "uptime_s":
			err = num(&f.UptimeSeconds)
		case "load":
			parts := strings.Fields(value)
			if len(parts) != 3 {
				return f, fmt.Errorf("the server gave the load as %q", value)
			}
			for i, dst := range []*float64{&f.Load1, &f.Load5, &f.Load15} {
				if *dst, err = strconv.ParseFloat(parts[i], 64); err != nil {
					return f, fmt.Errorf("the server gave the load as %q", value)
				}
			}
		case "machine":
			f.Machine = value
		case "app":
			f.Apps = append(f.Apps, value)
		default:
			return f, fmt.Errorf("the server printed a line tifa cannot read: %q", line)
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

// serverReport is one server of the file, as --all reports it.
type serverReport struct {
	Name    string       `json:"name"`
	ID      string       `json:"id"`
	Host    string       `json:"host"`
	Reached bool         `json:"reached"`
	Error   string       `json:"error,omitempty"`
	Facts   *serverFacts `json:"facts,omitempty"`
	Apps    []appReport  `json:"apps"`
}

// appReport is one app on a server. Deployed says a release is current. The fields after it are
// empty or 0 for an app that is not deployed, and Health is empty while the service does not run.
type appReport struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Deployed    bool   `json:"deployed"`
	Release     string `json:"release,omitempty"`
	Commit      string `json:"commit,omitempty"`
	CheckPassed bool   `json:"checkPassed"`
	Service     string `json:"service,omitempty"`
	ActiveSince string `json:"activeSince,omitempty"`
	Restarts    int    `json:"restarts"`
	Health      string `json:"health,omitempty"`
	MemoryKiB   int64  `json:"memoryKiB"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

// serverRef is the server an app status was asked on. Name and ID are known only when the host
// came from ~/.tifa/config.yaml, since only there is the machine ID of the server.
type serverRef struct {
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
	Host string `json:"host"`
}

// releaseReport is a release kept on the server, as the text status lists it.
type releaseReport struct {
	ID          string `json:"id"`
	Commit      string `json:"commit"`
	CheckPassed bool   `json:"checkPassed"`
}

// appStatus is what status --json prints for one app: the fields of an app of --all, the server
// and the kept releases.
type appStatus struct {
	appReport
	Server   serverRef       `json:"server"`
	Releases []releaseReport `json:"releases"`
}

// appStatusOf builds the status of the app of t from its probe s, with statusOf as --all does.
func appStatusOf(t deployTarget, s serverState) appStatus {
	srv := serverRef{Host: t.dest.raw}
	if t.dest.knownID != "" {
		srv.ID, srv.Name = t.dest.knownID, knownServer{ID: t.dest.knownID}.name()
	}
	out := appStatus{appReport: statusOf(t.app, t.dest.raw, s), Server: srv, Releases: []releaseReport{}}
	for _, id := range releaseOrder(s) {
		_, passed := s.good(id)
		out.Releases = append(out.Releases, releaseReport{ID: id, Commit: s.latest(id).Commit, CheckPassed: passed})
	}
	return out
}

// healthPasses judges an answer of GET healthPath the way a deploy does.
func healthPasses(code string) bool {
	return code != "" && code != "000" && code < "500"
}

// statusOf turns the probe of an app into its report.
func statusOf(name, host string, s serverState) appReport {
	a := appReport{Name: name, Host: host}
	if s.Port == 0 || s.Current == "" {
		a.OK = true
		return a
	}
	_, passed := s.good(s.Current)
	a.Deployed = true
	a.Release = s.Current
	a.Commit = s.latest(s.Current).Commit
	a.CheckPassed = passed
	a.Service = s.Active
	a.ActiveSince = s.ActiveSince
	a.Restarts = s.Restarts
	a.Health = s.Health
	a.MemoryKiB = s.RSS
	a.OK = passed && s.Active == "active" && healthPasses(s.Health)
	return a
}

// problems counts what is wrong in the report: servers that could not be checked, and the apps
// that are deployed and not well, or could not be asked.
func problems(servers []serverReport) (badServers, badApps int) {
	for _, s := range servers {
		if !s.Reached {
			badServers++
			continue
		}
		for _, a := range s.Apps {
			if !a.OK {
				badApps++
			}
		}
	}
	return
}

// checkServer asks one server: its facts, then each of its apps.
func checkServer(ctx context.Context, ks knownServer) serverReport {
	r := serverReport{Name: ks.name(), ID: ks.ID, Host: ks.Host, Apps: []appReport{}}
	ctx, cancel := context.WithTimeout(ctx, statusServerTimeout)
	defer cancel()
	hostOf := func(raw string) (deployTarget, error) {
		h, err := parseDeployHost(raw)
		if err != nil {
			return deployTarget{}, err
		}
		h.knownID = ks.ID
		return deployTarget{host: h.addr(), dest: h}, nil
	}
	t, err := hostOf(ks.Host)
	if err != nil {
		r.Error = fmt.Sprintf("the host %q: %v", ks.Host, err)
		return r
	}
	var out, said bytes.Buffer
	if err := t.ssh(ctx, deployUser, factsScript(), nil, &out, &said); err != nil {
		r.Error = withSaid(err, said.String())
		return r
	}
	facts, err := parseServerFacts(out.String())
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if facts.Machine != ks.ID {
		r.Error = fmt.Sprintf("the host now points at another machine: %s says it is %q, and the entry is %q", ks.Host, facts.Machine, ks.ID)
		return r
	}
	r.Reached, r.Facts = true, &facts
	hosts := map[string]string{}
	for _, a := range ks.Apps {
		if _, dup := hosts[a.Name]; !dup {
			hosts[a.Name] = cmpOr(a.Host, ks.Host)
		}
	}
	names := slices.Clone(facts.Apps)
	for name := range hosts {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		host := cmpOr(hosts[name], ks.Host)
		at, err := hostOf(host)
		if err == nil {
			at.app = name
			var s serverState
			var said bytes.Buffer
			s, err = at.probe(ctx, statusScript(name), &said)
			if err == nil {
				r.Apps = append(r.Apps, statusOf(name, host, s))
				continue
			}
			err = errors.New(withSaid(err, said.String()))
		}
		r.Apps = append(r.Apps, appReport{Name: name, Host: host, Error: err.Error()})
	}
	return r
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// withSaid adds the last line ssh printed to its error.
func withSaid(err error, said string) string {
	base := err.Error()
	var login *sshLoginError
	if errors.As(err, &login) {
		base = fmt.Sprintf("ssh %s@%s failed (%v)", login.user, login.host, login.err)
	}
	said = strings.TrimSpace(said)
	if i := strings.LastIndexByte(said, '\n'); i >= 0 {
		said = said[i+1:]
	}
	if said == "" {
		return base
	}
	return base + ": " + said
}

// checkServers asks the servers, statusAllParallel at a time, and returns them in the order given.
func checkServers(ctx context.Context, servers []knownServer) []serverReport {
	out := make([]serverReport, len(servers))
	var wg sync.WaitGroup
	gate := make(chan struct{}, statusAllParallel)
	for i, ks := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			out[i] = checkServer(ctx, ks)
		}()
	}
	wg.Wait()
	return out
}

// deployStatusAll prints the state of every server of ~/.tifa/config.yaml, as text or JSON. It
// returns an error when a server could not be reached or an app is not well.
func deployStatusAll(ctx context.Context, asJSON bool, stdout io.Writer) error {
	servers, err := readServers()
	if err != nil {
		return fmt.Errorf("%s could not be read: %w", shownServersPath(), err)
	}
	if len(servers) == 0 {
		if asJSON {
			_, err := fmt.Fprintln(stdout, "[]")
			return err
		}
		fmt.Fprintf(stdout, "No server was set up from this machine: %s lists none. Set one up with go tool tifa deploy setup.\n", shownServersPath())
		return nil
	}
	report := checkServers(ctx, servers)
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprint(stdout, statusAllText(report, deployNow()))
	}
	badServers, badApps := problems(report)
	if badServers == 0 && badApps == 0 {
		return nil
	}
	var parts []string
	if badServers > 0 {
		parts = append(parts, count(badServers, "server", "servers")+" could not be checked")
	}
	if badApps > 0 {
		parts = append(parts, count(badApps, "app is", "apps are")+" not well")
	}
	return errors.New(strings.Join(parts, " and "))
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// statusAllText is the text --all prints at the time now.
func statusAllText(servers []serverReport, now time.Time) string {
	var b strings.Builder
	for _, s := range servers {
		if s.Name == s.Host {
			fmt.Fprintf(&b, "%s\n", s.Name)
		} else {
			fmt.Fprintf(&b, "%s (%s)\n", s.Name, s.Host)
		}
		line := func(indent, label, text string) { fmt.Fprintf(&b, "%s%-8s  %s\n", indent, label, text) }
		if !s.Reached {
			line("  ", "problem", s.Error)
			b.WriteString("\n")
			continue
		}
		f := s.Facts
		system := cmpOr(f.OS, "unknown system") + ", " + cmpOr(f.Arch, "unknown arch")
		if f.CPUs > 0 {
			system += ", " + count(f.CPUs, "CPU", "CPUs")
		}
		line("  ", "system", system)
		if f.MemoryTotalKiB > 0 {
			line("  ", "memory", fmt.Sprintf("%s total, %s available", sizeKiB(f.MemoryTotalKiB), sizeKiB(f.MemoryAvailKiB)))
		}
		if f.DiskTotalKiB > 0 {
			line("  ", "disk", fmt.Sprintf("%s used of %s (%d%%)", sizeKiB(f.DiskUsedKiB), sizeKiB(f.DiskTotalKiB), f.DiskUsedKiB*100/f.DiskTotalKiB))
		}
		if f.UptimeSeconds > 0 {
			line("  ", "uptime", fmt.Sprintf("%s, load %.2f %.2f %.2f", uptimeText(f.UptimeSeconds), f.Load1, f.Load5, f.Load15))
		}
		if len(s.Apps) == 0 {
			line("  ", "apps", "none")
		}
		for _, a := range s.Apps {
			fmt.Fprintf(&b, "\n  %s\n", a.Name)
			in := "    "
			switch {
			case a.Error != "":
				line(in, "problem", a.Error)
				continue
			case !a.Deployed:
				line(in, "release", "not deployed yet: run go tool tifa deploy")
				continue
			}
			check := "failed its health check"
			if a.CheckPassed {
				check = "passed its health check"
			}
			line(in, "release", fmt.Sprintf("%s  commit %s  %s", a.Release, a.Commit, check))
			service := a.Service
			if service == "" {
				service = "unknown: systemd did not say"
			}
			if a.Service == "active" && a.ActiveSince != "" {
				service = "active since " + a.ActiveSince
				if since, err := time.Parse("Mon 2006-01-02 15:04:05 MST", a.ActiveSince); err == nil {
					service = fmt.Sprintf("active since %s, %s ago", since.Format("2006-01-02 15:04:05 MST"), ago(now.Sub(since)))
				}
			}
			line(in, "service", fmt.Sprintf("%s, %s", service, count(a.Restarts, "restart", "restarts")))
			line(in, "health", healthLine(a.Health))
			if a.MemoryKiB > 0 {
				line(in, "memory", fmt.Sprintf("%.1f MB", float64(a.MemoryKiB)*1024/1e6))
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Only the servers this machine has set up or deployed to, the ones in %s, are shown.\n", shownServersPath())
	return b.String()
}

// sizeKiB writes a size given in KiB in GB, or MB below one GB.
func sizeKiB(kib int64) string {
	b := float64(kib) * 1024
	if b >= 1e9 {
		return fmt.Sprintf("%.1f GB", b/1e9)
	}
	return fmt.Sprintf("%.0f MB", b/1e6)
}

// uptimeText writes seconds as days, hours and minutes.
func uptimeText(sec int64) string {
	d, h, m := sec/86400, sec%86400/3600, sec%3600/60
	switch {
	case d > 0:
		return fmt.Sprintf("up %dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("up %dh %dm", h, m)
	}
	return fmt.Sprintf("up %dm", m)
}
