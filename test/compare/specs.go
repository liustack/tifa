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
	ConfigEngine  = "engine"
	ConfigAA      = "aa"
)

// Configs are the configurations that tables are made for, in the order they are measured.
var Configs = []string{ConfigDefault, ConfigEngine}

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
	Version   string   // package.json, from Dir, of the framework package whose version the record keeps
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
			Framework: "tifa", Label: "Tifa", Configs: configs,
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
			Framework: "nextjs", Label: label, Configs: configs, Reference: reference,
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
			Framework: "start", Label: label, Configs: configs, Reference: reference,
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
			Framework: "reactrouter", Label: label, Configs: configs, Reference: reference,
			Version: filepath.Join("node_modules", "react-router", "package.json"),
		}
	}
	both := []string{ConfigDefault, ConfigEngine}
	engine := []string{ConfigEngine}
	return []Spec{
		tifa("tifa", []string{ConfigDefault}),
		tifaCacheOff,
		next("nextjs", "Next.js", false, both),
		next("nextjs-jitless", "Next.js --jitless", true, engine, "--jitless"),
		start("start", "TanStack Start", false, both),
		start("start-jitless", "TanStack Start --jitless", true, engine, "--jitless"),
		reactRouter("reactrouter", "React Router", false, both),
		reactRouter("reactrouter-jitless", "React Router --jitless", true, engine, "--jitless"),
	}
}

// Targets are the servers of Specs, for the tests.
func (e Env) Targets() []Target {
	var out []Target
	for _, s := range e.Specs() {
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
