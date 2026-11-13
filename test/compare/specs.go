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
			Create:      func(project string) string { return "/projects/" + project + "/orders" },
			Metrics:     "/metrics",
			StoreWrite:  SameStorePath,
			EditProduct: func(project, product string) string { return "/projects/" + project + "/products/" + product },
			// The data of the product list alone (the page, not its Layouts), as the browser router asks.
			Products: func(project, buildID string) string {
				return "/projects/" + project + "/products?_tifa=" + buildID + ".001"
			},
		}
	}
	next := e.nextSpec
	start := func(name, label string, reference bool, configs []string, flags ...string) Spec {
		return Spec{
			Target: Target{
				Name: name, Dir: e.Start, Prog: "node",
				Args: append(slices.Clone(flags), filepath.Join(".output", "server", "index.mjs")),
				Env:  []string{"NODE_ENV=production", "HOST=127.0.0.1", "PORT=%PORT%"},
			},
			Framework: "start", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: SameStorePath, EditProduct: nodeEditProduct, Products: nodeProducts,
			Version: filepath.Join("node_modules", "@tanstack", "react-start", "package.json"),
		}
	}
	tifaCacheOff := tifa("tifa-nocache", []string{ConfigEngine}, "--ssr-memo-size", "0")
	tifaCacheOff.Label = "Tifa (cache off)"
	reactRouter := func(name, label string, reference bool, configs []string, flags ...string) Spec {
		return Spec{
			Target: Target{
				Name: name, Dir: e.React, Prog: "node",
				Args: append(slices.Clone(flags), filepath.Join("node_modules", "@react-router", "serve", "bin.cjs"), filepath.Join("build", "server", "index.js")),
				Env:  []string{"NODE_ENV=production", "HOST=127.0.0.1", "PORT=%PORT%"},
			},
			Framework: "reactrouter", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: ReactRouterStorePath, EditProduct: nodeEditProduct, Products: nodeProducts,
			Version: filepath.Join("node_modules", "react-router", "package.json"),
		}
	}
	nextCompression := func(name, label, encoding string) Spec {
		s := next(name, label, true, []string{ConfigCompression})
		s.AcceptEncoding = encoding
		return s
	}
	// The two references of the browse comparison that are a Node framework with a setting it does not
	// ship with. Start with gzip is the same build as Start, started with COMPARE_GZIP=1, and the browser
	// sends it what it sends Start, so it replays the trace of Start. Next.js with no prefetch is a
	// build of its own, whose pages make the browser send other requests: it is a framework of its own
	// here, with a trace of its own.
	startGzip := start("start-gzip", "TanStack Start, gzip", true, nil)
	startGzip.Env = append(startGzip.Env, "COMPARE_GZIP=1")
	nextNoPrefetch := next(NextNoPrefetch, "Next.js, no prefetch", true, nil)
	nextNoPrefetch.Framework = NextNoPrefetch
	nextNoPrefetch.Env = append(nextNoPrefetch.Env, "COMPARE_NEXT_DIST="+NextNoPrefetchDist)
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
		startGzip,
		nextNoPrefetch,
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

// NextNoPrefetch is the id of the reference server that is the Next.js app built with prefetching
// off (NEXT_PUBLIC_COMPARE_PREFETCH=off), and NextNoPrefetchDist the folder of that build in the app.
const (
	NextNoPrefetch     = "nextjsnoprefetch"
	NextNoPrefetchDist = ".next-noprefetch"
)

// BrowseFrameworks are the specs the browse comparison runs: each framework as shipped, then the
// references, which are not compared (browseReferences): Tifa with its render memo off, Start with
// gzip added and Next.js with prefetching off.
var BrowseFrameworks = []string{"tifa", "nextjs", "start", "reactrouter", "tifa-nocache", "start-gzip", NextNoPrefetch}
