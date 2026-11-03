package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/liustack/slowjs"
)

// takeReads is what returns, and forgets, the reads readcheck.js wrote down.
const takeReads = "globalThis.__tifa_reads__()"

// Reads collects, under tifa dev, where the code of the app read the time or a random number in
// the renders of a context that WithReads gives. The place of a read is the first frame of its
// stack in a source of the app: not in node_modules and not in .tifa, so the reads of React,
// TanStack Router and the runtime of tifa are left out, and a read inside a package is named by the
// call of the app that led to it.
type Reads struct {
	mu     sync.Mutex
	places []string
}

type readsKey struct{}

// WithReads returns a context whose renders, on a pool of tifa dev, write their reads into the
// Reads it returns.
func WithReads(ctx context.Context) (context.Context, *Reads) {
	r := &Reads{}
	return context.WithValue(ctx, readsKey{}, r), r
}

// Places lists the reads, each as what was read and where, such as "Date.now() at app/page.tsx:4:9",
// each once, in the order of the first read.
func (r *Reads) Places() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.places...)
}

func (r *Reads) add(place string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.places {
		if p == place {
			return
		}
	}
	r.places = append(r.places, place)
}

// read is a read readcheck.js wrote down: what was read, and the stack of the read.
type read struct {
	Call  string `json:"call"`
	Stack string `json:"stack"`
}

// takeReads takes the reads of the render vm just ran, which leaves the list empty for the next
// one, and adds those of the app to the Reads of ctx, if it has one.
func (p *Pool) takeReads(ctx context.Context, vm *slowjs.VM) error {
	if err := vm.SetEvalTimeout(p.timeout); err != nil {
		return err
	}
	value, err := vm.Eval(takeReads, slowjs.EvalGlobal)
	if err != nil {
		return err
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("got %T, want a JSON string", value)
	}
	sink, _ := ctx.Value(readsKey{}).(*Reads)
	if sink == nil {
		return nil
	}
	var reads []read
	if err := json.Unmarshal([]byte(text), &reads); err != nil {
		return err
	}
	for _, r := range reads {
		if place, ok := p.sources.appPlace(r.Stack); ok {
			sink.add(r.Call + " at " + place)
		}
	}
	return nil
}

// appPlace returns the place in the sources of the first frame of stack in code of the app, and
// false when no frame is: the bundle has no source map, or only the framework and packages read.
func (s *sources) appPlace(stack string) (string, bool) {
	for _, place := range bundlePlace.FindAllString(stack, maxFrames) {
		line, column := placeOf(place)
		file, sourceLine, sourceColumn, ok := s.source(line, column)
		if !ok || frameworkFile(file) {
			continue
		}
		return fmt.Sprintf("%s:%d:%d", file, sourceLine+1, sourceColumn+1), true
	}
	return "", false
}

// frameworkFile reports whether a source of the bundle is code the app did not write: a package,
// or what tifa build generates and copies into .tifa, the runtime among it.
func frameworkFile(file string) bool {
	return strings.HasPrefix(file, ".tifa/") || strings.HasPrefix(file, "node_modules/") || strings.Contains(file, "/node_modules/")
}
