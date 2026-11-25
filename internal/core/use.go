package core

import (
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"time"
)

type loadCall struct {
	done  chan struct{}
	value any
	err   error
}

// Use returns the result of load for this request, running it at most once.
// A caller that arrives while load is running waits for that run.
func Use[T any](c *Ctx, load func(*Ctx) (T, error)) (T, error) {
	// Function values are not comparable, so the code pointer identifies the Load. This holds for the
	// package-level Load functions of routes and Layouts. Closures of one literal, and method values of
	// one method, share a code pointer and would share a result.
	return useKeyed(c, reflect.ValueOf(load).Pointer(), load)
}

// LoadInput wraps a Load or Layout function that takes its query parameters as a second parameter:
// the returned function decodes the query string into a Q (see BindQuery, a value that is not valid
// answers 400 and the Load does not run) and runs load once per request, in the same table as the
// Loads that go through Use. The key is the code pointer of load itself, so two Loads never share
// one through the wrapper, which is a closure of this one function. The generated app.go makes the
// wrapper for every such function when the program starts.
func LoadInput[T, Q any](load func(*Ctx, Q) (T, error)) func(*Ctx) (any, error) {
	key := reflect.ValueOf(load).Pointer()
	return func(c *Ctx) (any, error) {
		return useKeyed(c, key, func(c *Ctx) (T, error) {
			var q Q
			if err := BindQuery(c, &q); err != nil {
				var zero T
				return zero, err
			}
			return load(c, q)
		})
	}
}

// useKeyed is Use for the Load that key names.
func useKeyed[T any](c *Ctx, key uintptr, load func(*Ctx) (T, error)) (T, error) {
	var zero T
	if key == c.load {
		return zero, errors.New("tifa: a Load cannot Use itself")
	}
	c.req.mu.Lock()
	call, running := c.req.loads[key]
	if !running {
		call = &loadCall{done: make(chan struct{})}
		c.req.loads[key] = call
		c.req.startLoad()
	}
	c.req.mu.Unlock()
	if running {
		<-call.done
	} else {
		lc := *c
		lc.mode, lc.load = modeLoad, key
		call.value, call.err = run(&lc, load)
		close(call.done)
	}
	if call.err != nil {
		return zero, call.err
	}
	value, _ := call.value.(T) // a nil interface result stays the zero value
	return value, nil
}

// startLoad notes that a Load of the request starts now, unless one started before. The caller
// holds r.mu.
func (r *request) startLoad() {
	if r.loadsStart.IsZero() {
		r.loadsStart = time.Now()
	}
}

// StartLoads notes that the Loads of a page start now and returns when the first Load of the
// request started: now, or earlier for a Load that OnRequest ran through Use. The data of the page
// is no older than that.
func StartLoads(c *Ctx) time.Time {
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	c.req.startLoad()
	return c.req.loadsStart
}

func run[T any](c *Ctx, load func(*Ctx) (T, error)) (value any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tifa: panic in Load: %v\n%s", p, debug.Stack())
		}
	}()
	return load(c)
}
