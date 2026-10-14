package devserver

import (
	"context"
	"fmt"
	"path"
	"time"
)

// Pipeline is the build work tifa dev drives. The tifa CLI implements it with static analysis,
// code generation, Vite and go build. Each method works on the whole application. A returned
// error is shown to the developer as is, in the terminal and in the browser overlay.
type Pipeline interface {
	// Analyze runs static analysis and returns a fingerprint of the route manifest:
	// the same routes give the same fingerprint, any change to the manifest gives a new one.
	Analyze(ctx context.Context) (fingerprint string, err error)
	// Generate writes the generated Go and TypeScript from the last analysis.
	Generate(ctx context.Context) error
	// Bundle builds the front end: under tifa dev the SSR bundle, while Vite's dev server serves
	// the browser modules. A detail that is not empty goes at the end of the step's log line.
	Bundle(ctx context.Context) (detail string, err error)
	// GoBuild builds the application executable that Options.App runs.
	GoBuild(ctx context.Context) error
}

type op int

const (
	created op = iota
	modified
	removed
)

func (o op) String() string { return [...]string{"created", "modified", "removed"}[o] }

// change is one file whose content differs from the last build. path is relative to the
// application directory and uses forward slashes.
type change struct {
	path string
	op   op
}

// steps is the work one rebuild does. Steps always run in the order of the fields.
type steps struct{ analyze, generate, bundle, goBuild bool }

var everything = steps{true, true, true, true}

func (s steps) or(t steps) steps {
	return steps{s.analyze || t.analyze, s.generate || t.generate, s.bundle || t.bundle, s.goBuild || t.goBuild}
}

type kind int

const (
	other    kind = iota
	goSource      // .go, go.mod, go.sum
	frontend      // TypeScript, JavaScript, CSS and JSON imported by them
	config        // tifa.yaml or tifa.json in the application directory
)

func kindOf(p string) kind {
	switch p {
	case "tifa.yaml", "tifa.json":
		return config
	case "go.mod", "go.sum":
		return goSource
	}
	switch path.Ext(p) {
	case ".go":
		return goSource
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".css", ".json":
		return frontend
	}
	return other
}

// plan decides the steps for a batch of changes:
//   - TypeScript, JavaScript and CSS only need a new bundle.
//   - Go needs a new analysis and a go build. The builder adds code generation and a bundle
//     when the analysis returns a new route manifest.
//   - A route.tsx or layout.tsx that appears or disappears changes the manifest, so does tifa.yaml.
func plan(changes []change) steps {
	var s steps
	for _, c := range changes {
		switch kindOf(c.path) {
		case goSource:
			s.analyze, s.goBuild = true, true
		case config:
			s = everything
		case frontend:
			s.bundle = true
			base := path.Base(c.path)
			if c.op != modified && (base == "route.tsx" || base == "layout.tsx") {
				s = everything
			}
		}
	}
	return s
}

// builder runs the steps of each rebuild against the pipeline and remembers what the next one needs.
type builder struct {
	p        Pipeline
	log      func(format string, args ...any) // one line per step that ran
	manifest string                           // fingerprint of the last successful analysis
	pending  steps                            // steps a failed rebuild left undone, run again with the next one
}

// step runs one pipeline step, logs how long it took and names the step in its error.
func (b *builder) step(name string, run func() error) error {
	return b.stepDetail(name, func() (string, error) { return "", run() })
}

// stepDetail is step for a step that adds a detail to its log line.
func (b *builder) stepDetail(name string, run func() (string, error)) error {
	start := time.Now()
	detail, err := run()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if detail != "" {
		detail = " (" + detail + ")"
	}
	b.log("%s done in %s%s", name, time.Since(start).Round(time.Millisecond), detail)
	return nil
}

// build runs s plus the steps a failed rebuild left undone, and returns the steps that ran. An
// analysis that returns a new fingerprint adds code generation, the bundle and go build.
// beforeBundle, when not nil, is called before the bundle step runs.
func (b *builder) build(ctx context.Context, s steps, beforeBundle func()) (steps, error) {
	s = s.or(b.pending)
	b.pending = s
	ran := s
	if s.analyze {
		var fp string
		err := b.step("static analysis", func() (err error) {
			fp, err = b.p.Analyze(ctx)
			return err
		})
		if err != nil {
			return steps{}, err
		}
		if fp != b.manifest {
			b.log("route manifest changed")
			s.generate, s.bundle, s.goBuild = true, true, true
			ran = ran.or(s)
			b.manifest = fp
		} else {
			b.log("route manifest unchanged")
		}
		s.analyze = false
		b.pending = s
	}
	if s.generate {
		if err := b.step("code generation", func() error { return b.p.Generate(ctx) }); err != nil {
			return steps{}, err
		}
		s.generate = false
		b.pending = s
	}
	if s.bundle {
		if beforeBundle != nil {
			beforeBundle()
		}
		if err := b.stepDetail("bundle", func() (string, error) { return b.p.Bundle(ctx) }); err != nil {
			return steps{}, err
		}
		s.bundle = false
		b.pending = s
	}
	if s.goBuild {
		if err := b.step("go build", func() error { return b.p.GoBuild(ctx) }); err != nil {
			return steps{}, err
		}
	}
	b.pending = steps{}
	return ran, nil
}
