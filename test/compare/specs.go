package compare

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// The configurations a spec can belong to. "default" is each framework as shipped. "engine" has
// every cache off, so each request does all its work, and holds the references that explain a gap.
// "aa" is the Tifa against Tifa check and belongs to no table.
const (
	ConfigDefault = "default"
	// ConfigCompression holds Next.js asked for gzip and for no compression, to tell what compressing
	// costs a server that does it. It belongs to no table.
	ConfigCompression = "compression"
	ConfigEngine      = "engine"
	ConfigAA          = "aa"
)

// Configs are the configurations that tables are made for, in the order they are measured.
var Configs = []string{ConfigDefault, ConfigEngine}

// AllConfigs are all the configurations that `measure -all` runs.
var AllConfigs = []string{ConfigDefault, ConfigEngine, ConfigCompression}

// Spec is one server to measure: a Target, which framework it is, and which configurations it
// stands for. A server that is the same in two configurations is one Spec in both and runs once
// per round. To add a framework, add its Specs here: the measuring, the report and the diff take
// whatever this lists. Name (of the Target) is the id of the spec.
type Spec struct {
	Target
	Framework string   // the app: "tifa", "nextjs", "start", "reactrouter"
	Label     string   // in the tables
	Configs   []string // the tables it is in
	Reference bool     // marked reference in the tables: it explains a gap and is not a contender
	// AcceptEncoding, when set, is what the client asks this spec for, instead of the global setting.
	AcceptEncoding string
	Write          Writer                      // the path of the write that changes the data of an order
	Create         func(project string) string // the path that takes the POST which places an order in a project
	StoreWrite     func(path string) string    // the path in this app of a write of the storefront, from its path in the shop
	Version        string                      // package.json, from Dir, of the framework package whose version the record keeps
}

// Specs are the servers to measure, in the order of a round before it is rotated. Tifa runs one
// VM on one OS thread, as the Node servers run one process, and in production mode.
func (e Env) Specs() []Spec {
	tifa := func(name string, configs []string, extra ...string) Spec {
		return Spec{
			Target: Target{
				Name: name, Dir: e.Shop, Prog: filepath.Join(e.Shop, "bin", "shop"),
				Args: append([]string{"--server-addr", "127.0.0.1:%PORT%", "--ssr-vms", "1"}, extra...),
				Env:  []string{"GOMAXPROCS=1", "SHOP_OPEN=1"},
			},
			Framework: "tifa", Label: "Tifa", Configs: configs, Write: func(project, order string) string { return "/projects/" + project + "/orders/" + order },
			Create:     func(project string) string { return "/projects/" + project + "/orders" },
			StoreWrite: SameStorePath,
		}
	}
	next := func(name, label string, reference bool, configs []string, flags ...string) Spec {
		nextBin := filepath.Join("node_modules", "next", "dist", "bin", "next")
		return Spec{
			Target: Target{
				Name: name, Dir: e.Next, Prog: "node",
				Args: append(slices.Clone(flags), nextBin, "start", "-H", "127.0.0.1", "-p", "%PORT%"),
				Env:  []string{"NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1"},
			},
			Framework: "nextjs", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: NextStorePath,
			Version: filepath.Join("node_modules", "next", "package.json"),
		}
	}
	start := func(name, label string, reference bool, configs []string, flags ...string) Spec {
		return Spec{
			Target: Target{
				Name: name, Dir: e.Start, Prog: "node",
				Args: append(slices.Clone(flags), filepath.Join(".output", "server", "index.mjs")),
				Env:  []string{"NODE_ENV=production", "HOST=127.0.0.1", "PORT=%PORT%"},
			},
			Framework: "start", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: SameStorePath,
			Version: filepath.Join("node_modules", "@tanstack", "react-start", "package.json"),
		}
	}
	tifaCacheOff := tifa("tifa-nocache", []string{ConfigEngine}, "--cache-size", "0")
	tifaCacheOff.Label = "Tifa (cache off)"
	reactRouter := func(name, label string, reference bool, configs []string, flags ...string) Spec {
		return Spec{
			Target: Target{
				Name: name, Dir: e.React, Prog: "node",
				Args: append(slices.Clone(flags), filepath.Join("node_modules", "@react-router", "serve", "bin.cjs"), filepath.Join("build", "server", "index.js")),
				Env:  []string{"NODE_ENV=production", "HOST=127.0.0.1", "PORT=%PORT%"},
			},
			Framework: "reactrouter", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: ReactRouterStorePath,
			Version: filepath.Join("node_modules", "react-router", "package.json"),
		}
	}
	nextCompression := func(name, label, encoding string) Spec {
		s := next(name, label, true, []string{ConfigCompression})
		s.AcceptEncoding = encoding
		return s
	}
	both := []string{ConfigDefault, ConfigEngine}
	engine := []string{ConfigEngine}
	return []Spec{
		tifa("tifa", []string{ConfigDefault}),
		tifaCacheOff,
		next("nextjs", "Next.js", false, both),
		next("nextjs-jitless", "Next.js --jitless", true, engine, "--jitless"),
		start("start", "TanStack Start", false, both),
		nextCompression("nextjs-identity", "Next.js, identity", "identity"),
		nextCompression("nextjs-gzip", "Next.js, gzip", "gzip"),
		start("start-jitless", "TanStack Start --jitless", true, engine, "--jitless"),
		reactRouter("reactrouter", "React Router", false, both),
		reactRouter("reactrouter-jitless", "React Router --jitless", true, engine, "--jitless"),
	}
}

// Targets are the servers of Specs, for the tests.
func (e Env) Targets() []Target {
	var out []Target
	for _, s := range e.Specs() {
		if s.AcceptEncoding != "" {
			continue // the same server as another spec, asked differently
		}
		out = append(out, s.Target)
	}
	return out
}

// Target returns the target of a spec by its id.
func (e Env) Target(name string) (Target, error) {
	for _, s := range e.Specs() {
		if s.Name == name {
			return s.Target, nil
		}
	}
	return Target{}, fmt.Errorf("no spec named %q", name)
}

// AASpecs are the two servers of the A/A check: the default Tifa twice.
func (e Env) AASpecs() []Spec {
	var tifa Spec
	for _, s := range e.Specs() {
		if s.Name == "tifa" {
			tifa = s
		}
	}
	a, b := tifa, tifa
	a.Name, b.Name = "tifa-aa1", "tifa-aa2"
	a.Configs, b.Configs = []string{ConfigAA}, []string{ConfigAA}
	return []Spec{a, b}
}

// packageVersion reads the version of an npm package from its package.json.
func packageVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var p struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &p) != nil {
		return ""
	}
	return p.Version
}

// nodeWrite is the path of the write endpoint of the three Node apps. It is one path for all of
// them, since Next.js cannot put a route handler next to a page.
func nodeWrite(project, order string) string { return "/compare-write/" + project + "/" + order }

// nodeCreate is the path that places an order in a project in the three Node apps.
func nodeCreate(project string) string { return "/compare-write/" + project }
