package core

import (
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
)

type loadCall struct {
	done  chan struct{}
	value any
	err   error
}

// Use returns the result of load for this request, running it at most once.
// A caller that arrives while load is running waits for that run.
func Use[T any](c *Ctx, load func(*Ctx) (T, error)) (T, error) {
	var zero T
	// Function values are not comparable, so the code pointer identifies the Load. This holds for the
	// package-level Load functions of routes and Layouts. Closures of one literal, and method values of
	// one method, share a code pointer and would share a result.
	key := reflect.ValueOf(load).Pointer()
	if key == c.load {
		return zero, errors.New("tifa: a Load cannot Use itself")
	}
	c.req.mu.Lock()
	call, running := c.req.loads[key]
	if !running {
		call = &loadCall{done: make(chan struct{})}
		c.req.loads[key] = call
	}
	c.req.mu.Unlock()
	if running {
		<-call.done
	} else {
		lc := *c
		lc.mode, lc.next, lc.load = modeLoad, nil, key
		call.value, call.err = run(&lc, load)
		close(call.done)
	}
	if call.err != nil {
		return zero, call.err
	}
	value, _ := call.value.(T) // a nil interface result stays the zero value
	return value, nil
}

func run[T any](c *Ctx, load func(*Ctx) (T, error)) (value any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tifa: panic in Load: %v\n%s", p, debug.Stack())
		}
	}()
	return load(c)
}
