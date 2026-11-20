package main

// While deploy setup builds its plan, the same read can be asked for more than once: findZone walks
// the same names twice, and several steps read the same tunnel configuration. The plan runs with a
// memo in its context, so a read with the same arguments runs once. The apply phase gets a context
// without the memo, because its steps change something and then read it back.

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
)

type readMemoKey struct{}

// readMemo holds the answers of the reads of one plan. A read that is under way is waited for, not
// started twice, and an error is remembered like an answer.
type readMemo struct {
	mu    sync.Mutex
	calls map[string]*memoCall
}

type memoCall struct {
	done chan struct{}
	val  any
	err  error
}

// withReadMemo returns ctx with a new, empty memo.
func withReadMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, readMemoKey{}, &readMemo{calls: map[string]*memoCall{}})
}

// memoRead runs run once per key when ctx carries a memo, and every time when it does not.
func memoRead[T any](ctx context.Context, key string, run func() (T, error)) (T, error) {
	m, _ := ctx.Value(readMemoKey{}).(*readMemo)
	if m == nil {
		return run()
	}
	m.mu.Lock()
	c, ok := m.calls[key]
	if !ok {
		c = &memoCall{done: make(chan struct{})}
		m.calls[key] = c
	}
	m.mu.Unlock()
	if ok {
		select {
		case <-c.done:
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	} else {
		var v T
		var err error
		func() {
			defer close(c.done)
			v, err = run()
			c.val, c.err = v, err
		}()
		return v, err
	}
	v, _ := c.val.(T)
	return v, c.err
}

// cfReads and ghReads are the only commands the memo remembers: the ones that read and change
// nothing. A command that is not listed always runs. tunnels token get is left out on purpose: it
// prints a secret.
var cfReads = [][]string{
	{"auth", "whoami"},
	{"zones", "list"},
	{"tunnels", "get"},
	{"tunnels", "list"},
	{"tunnels", "config", "get"},
	{"dns", "records", "list"},
	{"rulesets", "entrypoint", "get"},
	{"zero-trust", "access", "applications", "list"},
	{"zero-trust", "access", "policies", "list"},
	{"zero-trust", "access", "service-tokens", "list"},
}

var ghReads = [][]string{
	{"auth", "status"},
	{"repo", "view"},
	{"secret", "list"},
	{"variable", "list"},
}

func isRead(list [][]string, args []string) bool {
	return slices.ContainsFunc(list, func(c []string) bool {
		return len(args) >= len(c) && slices.Equal(args[:len(c)], c)
	})
}

// readCF is runCF for a call that reads: inside a plan, the same read runs once.
func readCF(ctx context.Context, args ...string) ([]byte, error) {
	if !isRead(cfReads, args) {
		return runCF(ctx, args...)
	}
	return memoRead(ctx, "cf\x00"+strings.Join(args, "\x00"), func() ([]byte, error) { return runCF(ctx, args...) })
}

// readGH is runGH for a call that reads: inside a plan, the same read in the same folder runs once.
// A call with an input is a write.
func readGH(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	if stdin != nil || !isRead(ghReads, args) {
		return runGH(ctx, dir, stdin, args...)
	}
	return memoRead(ctx, "gh\x00"+dir+"\x00"+strings.Join(args, "\x00"), func() ([]byte, error) { return runGH(ctx, dir, nil, args...) })
}
