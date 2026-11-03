package devserver

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"strings"
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
	// Embedded reports whether the Go code of the application embeds the file rel with go:embed.
	// rel is relative to the application directory and uses forward slashes, and also names a
	// file that does not exist yet but that a pattern would take. It answers for the last analysis
	// that worked, so call it after Analyze returned without an error. The function is never nil
	// and may be called from any goroutine.
	Embedded() func(rel string) bool
	// PollFiles makes Vite's dev server poll the files too, once tifa dev's watcher found the
	// system short of watches. It may run while a step runs. polling reports whether the dev
	// server polls, it does not when its watcher takes no watch per file.
	PollFiles(ctx context.Context) (polling bool, err error)
}

type op int

const (
	created op = iota
	modified
	removed
	restarted // only for viteServer
)

func (o op) String() string { return [...]string{"created", "modified", "removed", "restarted"}[o] }

// change is one file whose content differs from the last build. path is relative to the
// application directory and uses forward slashes.
type change struct {
	path string
	op   op
}

// steps is the work one rebuild does. Steps always run in the order of the fields. restart builds
// nothing: the app starts again, as it does after every build but one of the bundle alone.
type steps struct{ analyze, generate, bundle, goBuild, restart bool }

var everything = steps{analyze: true, generate: true, bundle: true, goBuild: true}

func (s steps) or(t steps) steps {
	return steps{s.analyze || t.analyze, s.generate || t.generate, s.bundle || t.bundle, s.goBuild || t.goBuild, s.restart || t.restart}
}

type kind int

const (
	other    kind = iota
	goSource      // .go, go.mod, go.sum
	frontend      // TypeScript, JavaScript, CSS and JSON imported by them, and what Vite loads from outside the root
	config        // tifa.yaml in the application directory
	dotenv        // .env in the application directory, which the app reads when it starts
)

func kindOf(p string) kind {
	if strings.HasPrefix(p, "../") {
		// Outside the root: only Outside records such a file, one that Vite's dev server watches
		// because a module loaded it, such as source of the workspace or a text file imported with
		// ?raw. Whatever its name, the SSR bundle may contain it.
		return frontend
	}
	switch p {
	case "tifa.yaml":
		return config
	case "go.mod", "go.sum":
		return goSource
	case ".env":
		return dotenv
	}
	switch path.Ext(p) {
	case ".go":
		return goSource
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".css", ".json":
		return frontend
	}
	return other
}

// plan decides the steps for a batch of changes. embedded, when not nil, says which files the Go code
// embeds:
//   - TypeScript, JavaScript and CSS only need a new bundle.
//   - Go needs a new analysis and a go build. The builder adds code generation and a bundle
//     when the analysis returns a new route manifest.
//   - A page.tsx, layout.tsx or layout_.tsx that appears or disappears changes the manifest, so
//     does tifa.yaml.
//   - A file the Go code embeds is part of the program, whatever its kind, so it needs a go build.
//     A file that appears or disappears may change which files are embedded, so it needs the
//     analysis too. An embedded front end file needs the bundle as well.
//   - A restart of Vite's dev server needs a new bundle, as a file outside the root may have
//     changed meanwhile with no event.
//   - .env needs only a restart of the app, which reads it when it starts.
func plan(changes []change, embedded func(rel string) bool) steps {
	var s steps
	for _, c := range changes {
		if c.op == restarted {
			s.bundle = true
			continue
		}
		if embedded != nil && embedded(c.path) {
			s.goBuild = true
			if c.op != modified {
				s.analyze = true
			}
		}
		switch kindOf(c.path) {
		case goSource:
			s.analyze, s.goBuild = true, true
		case config:
			s = s.or(everything)
		case dotenv:
			s.restart = true
		case frontend:
			s.bundle = true
			base := path.Base(c.path)
			if c.op != modified && (base == "page.tsx" || base == "layout.tsx" || base == "layout_.tsx") {
				s = s.or(everything)
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
	// learn, when not nil, gets what Pipeline.Embedded answers after each analysis that worked.
	learn   func(embedded func(rel string) bool)
	pending steps // steps a failed rebuild left undone, run again with the next one
	// A step that takes longer than slow logs that it still runs, then again every every.
	// Zero means defaultSlow and defaultEvery.
	slow, every time.Duration
}

// The defaults of builder.slow and builder.every.
const (
	defaultSlow  = 5 * time.Second
	defaultEvery = 15 * time.Second
)

// step runs one pipeline step, logs how long it took and names the step in its error.
func (b *builder) step(name string, run func() error) error {
	return b.stepDetail(name, func() (string, error) { return "", run() })
}

// stepDetail is step for a step that adds a detail to its log line.
func (b *builder) stepDetail(name string, run func() (string, error)) error {
	start := time.Now()
	stop := b.progress(name, start)
	detail, err := run()
	stop()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if detail != "" {
		detail = " (" + detail + ")"
	}
	b.log("%s done in %s%s", name, time.Since(start).Round(time.Millisecond), detail)
	return nil
}

// progress logs that the step name started at start still runs, once it took slow and then
// every every. The first line of go build says why a first build is slow. The returned function
// stops it, and no line comes once it returned.
func (b *builder) progress(name string, start time.Time) func() {
	slow, every := cmp.Or(b.slow, defaultSlow), cmp.Or(b.every, defaultEvery)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		timer := time.NewTimer(slow)
		defer timer.Stop()
		why := ""
		if name == "go build" {
			why = ". The first build compiles QuickJS and the other dependencies, later builds use Go's build cache"
		}
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
			}
			b.log("%s still running (%s)%s", name, time.Since(start).Round(time.Second), why)
			why = ""
			timer.Reset(every)
		}
	}()
	return func() {
		close(stop)
		<-stopped
	}
}

// build runs s plus the steps a failed rebuild left undone, and returns the steps that ran. An
// analysis that returns a new fingerprint adds code generation, the bundle and go build.
// needBundle, when not nil, is called once it is known that the bundle step runs: at the start,
// or once the analysis finds a new fingerprint.
func (b *builder) build(ctx context.Context, s steps, needBundle func()) (steps, error) {
	s = s.or(b.pending)
	b.pending = s
	ran := s
	if s.bundle && needBundle != nil {
		needBundle()
	}
	if s.analyze {
		var fp string
		err := b.step("static analysis", func() (err error) {
			fp, err = b.p.Analyze(ctx)
			return err
		})
		if err != nil {
			return steps{}, err
		}
		if b.learn != nil {
			b.learn(b.p.Embedded())
		}
		if fp != b.manifest {
			b.log("route manifest changed")
			if !s.bundle && needBundle != nil {
				needBundle()
			}
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
