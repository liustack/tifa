package devserver

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"slices"
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
	// Content scans content/ again and converts the entries that changed into the files the app
	// reads, without go build: those whose Markdown changed, and those near one of the files of
	// changed (relative to the application directory with forward slashes), which also names the
	// folders that appeared or went. With all it converts
	// every entry, as after a change of the Vite config. It runs after Bundle, which starts Vite's
	// dev server. A detail that is not empty goes at the end of the step's log line.
	Content(ctx context.Context, all bool, changed []string) (detail string, err error)
	// HasContent reports whether the app has content collections, as the last analysis that worked
	// saw it. Content then runs after every analysis, which may have changed the collections.
	HasContent() bool
	// ContentLoaded says that the app loaded the content the last Content that worked wrote, when
	// it started or in place. The files of older content, which the app no longer reads, may go.
	ContentLoaded() error
	// HasPages reports whether the app has a page.tsx, as the last analysis that worked saw it.
	// Only then does Vite's dev server run.
	HasPages() bool
	// GoBuild builds the application executable that Options.App runs.
	GoBuild(ctx context.Context) error
	// Embedded reports whether the Go code of the application embeds the file rel with go:embed.
	// rel is relative to the application directory and uses forward slashes, and also names a
	// file that does not exist yet but that a pattern would take. It answers for the last analysis
	// that worked, so call it after Analyze returned without an error. The function is never nil
	// and may be called from any goroutine.
	Embedded() func(rel string) bool
	// Public returns the folder of static files, relative to the application directory with forward
	// slashes, empty when there is none or it lies outside the application directory. The generated
	// code lists its files, so one that appears or goes needs a new bundle. Like Embedded it
	// answers for the last analysis that worked, which reads tifa.yaml.
	Public() string
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

// steps is the work one rebuild does. Steps always run in the order of the fields. content
// converts the entries that changed, contentAll makes it convert every entry. restart builds
// nothing: the app starts again, as it does after every build but one of the bundle or the content
// alone, which the running app loads in place.
type steps struct{ analyze, generate, bundle, content, contentAll, goBuild, restart bool }

var everything = steps{analyze: true, generate: true, bundle: true, content: true, contentAll: true, goBuild: true}

func (s steps) or(t steps) steps {
	return steps{s.analyze || t.analyze, s.generate || t.generate, s.bundle || t.bundle, s.content || t.content,
		s.contentAll || t.contentAll, s.goBuild || t.goBuild, s.restart || t.restart}
}

// inPlace reports whether the running app can load what s built without a restart: a new SSR
// bundle, new content or both.
func (s steps) inPlace() bool {
	return (s.bundle || s.content) && !s.analyze && !s.generate && !s.goBuild && !s.restart
}

// contentDir is the folder of the content collections, see internal/content/scan.
const contentDir = "content/"

type kind int

const (
	other       kind = iota
	goSource         // .go, go.mod, go.sum
	frontend         // TypeScript, JavaScript, CSS and JSON imported by them, and what Vite loads from outside the root
	config           // tifa.yaml in the application directory
	dotenv           // .env in the application directory, which the app reads when it starts
	contentFile      // any file under content/: an entry of a collection or a file of one
)

func kindOf(p string) kind {
	if strings.HasPrefix(p, "../") {
		// Outside the root: only Outside records such a file, one that Vite's dev server watches
		// because a module loaded it, such as source of the workspace or a text file imported with
		// ?raw. Whatever its name, the SSR bundle may contain it.
		return frontend
	}
	if strings.HasPrefix(p, contentDir) {
		// Whatever its name, such as data.json or page.tsx: the content step reads it, Vite and
		// Go do not.
		return contentFile
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
//     changed meanwhile with no event, and every entry of the content converted again, as the
//     Vite config or a plugin it imports may have changed.
//   - A file under content/ needs the content step, whatever its name: the scan, and the
//     conversion of the entries it changes. It needs no go build, as the app reads the content
//     from its files.
//   - .env needs only a restart of the app, which reads it when it starts.
func plan(changes []change, embedded func(rel string) bool) steps {
	var s steps
	for _, c := range changes {
		if c.op == restarted {
			s.bundle, s.content, s.contentAll = true, true, true
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
		case contentFile:
			s.content = true
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

// inPublic reports whether rel is a file in the public folder, given relative to the application
// directory. An empty folder holds nothing.
func inPublic(public, rel string) bool {
	return public != "" && strings.HasPrefix(rel, path.Clean(public)+"/")
}

// planPublic is plan with the public folder: the generated code lists the files in it, so a file
// that appears or goes needs a new bundle, which generates the code again. Changing the content
// of one needs nothing more than before.
func planPublic(changes []change, embedded func(rel string) bool, public string) steps {
	s := plan(changes, embedded)
	for _, c := range changes {
		if c.op != modified && inPublic(public, c.path) {
			s.bundle = true
		}
	}
	return s
}

// planApp is planPublic for an app that has pages or not. An app without pages runs no dev server
// of Vite, which would restart on a change of its config: its front end files, outside the public
// folder, are the Vite config, the plugins that imports and package.json, so a change of one
// converts every entry of the content.
func planApp(changes []change, embedded func(rel string) bool, public string, pages bool) steps {
	s := planPublic(changes, embedded, public)
	if pages {
		return s
	}
	for _, c := range changes {
		if kindOf(c.path) == frontend && !inPublic(public, c.path) {
			s.content, s.contentAll = true, true
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
	// contentFiles are the files under content/ that changed since the last content step that
	// worked, for Pipeline.Content.
	contentFiles []string
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

// noteContent records the files of changes that are under content/, for the next content step.
func (b *builder) noteContent(changes []change) {
	for _, c := range changes {
		if strings.HasPrefix(c.path, contentDir) && !slices.Contains(b.contentFiles, c.path) {
			b.contentFiles = append(b.contentFiles, c.path)
		}
	}
}

// build runs s plus the steps a failed rebuild left undone, and returns the steps that ran. An
// analysis that returns a new fingerprint adds code generation, the bundle and go build, and in
// an app with content the content step. needBundle, when not nil, is called once it is known that
// the bundle or the content step runs: at the start, or once the analysis adds one.
func (b *builder) build(ctx context.Context, s steps, needBundle func()) (steps, error) {
	s = s.or(b.pending)
	b.pending = s
	ran := s
	held := false
	hold := func() {
		if !held && needBundle != nil {
			held = true
			needBundle()
		}
	}
	if s.bundle || s.content {
		hold()
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
			hold()
			s.generate, s.bundle, s.goBuild = true, true, true
			ran = ran.or(s)
			b.manifest = fp
		} else {
			b.log("route manifest unchanged")
		}
		// The analysis may have changed the collections or their schema types.
		if b.p.HasContent() {
			hold()
			s.content = true
			ran = ran.or(s)
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
	if s.content && (len(b.contentFiles) > 0 || b.p.HasContent()) {
		err := b.stepDetail("content", func() (string, error) { return b.p.Content(ctx, s.contentAll, b.contentFiles) })
		if err != nil {
			return steps{}, err
		}
		b.contentFiles = nil
	} else if s.content {
		// An app without collections and no file under content/ changed: nothing to scan.
		ran.content, ran.contentAll = false, false
	}
	s.content, s.contentAll = false, false
	b.pending = s
	if s.goBuild {
		if err := b.step("go build", func() error { return b.p.GoBuild(ctx) }); err != nil {
			return steps{}, err
		}
	}
	b.pending = steps{}
	return ran, nil
}
