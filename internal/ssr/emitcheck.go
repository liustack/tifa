//go:build tifa_emit_check

package ssr

// The test hook of the start and end tags written in C (tags.go), for test/e2e only: a program
// built with the tag tifa_emit_check, from a bundle that the tifa plugin built with
// TIFA_EMIT_CHECK=1, writes each tag both ways and reports how they compare. The bundle's React DOM
// writes the page with its own functions and hands each comparison to __tifa_emit_check:
//
//	__tifa_emit_check(verdict, tag, props, c, js)
//
// verdict is "same", "declined" (the C function left the element to React) or "differ", props
// names the keys of the props with the type of each value, and c and js are the two texts. The
// program prints a line for each difference, for the first declines of each tag, and a count after
// every 1,000 comparisons and once more when the program gets an interrupt, all starting with
// "tifa emit check: ".

import (
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/liustack/quickjs"
)

func init() {
	emitCheck = registerEmitCheck
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		emitCounts.Lock()
		defer emitCounts.Unlock()
		emitCounts.print()
	}()
}

type counts struct {
	sync.Mutex
	same, declined, differ int
	declinedTags           map[string]int
}

var emitCounts counts

// print prints the counts. The caller holds the lock.
func (c *counts) print() {
	fmt.Fprintf(os.Stderr, "tifa emit check: %d compared, %d the same, %d declined, %d differ\n",
		c.same+c.declined+c.differ, c.same, c.declined, c.differ)
}

func registerEmitCheck(vm *quickjs.VM) error {
	return vm.RegisterHostFunc("__tifa_emit_check", func(args []any) (any, error) {
		if len(args) != 5 {
			return nil, fmt.Errorf("__tifa_emit_check takes 5 arguments, got %d", len(args))
		}
		verdict, _ := args[0].(string)
		tag, _ := args[1].(string)
		props, _ := args[2].(string)
		c, _ := args[3].(string)
		js, _ := args[4].(string)
		emitCounts.Lock()
		defer emitCounts.Unlock()
		switch verdict {
		case "same":
			emitCounts.same++
		case "declined":
			emitCounts.declined++
			if emitCounts.declinedTags == nil {
				emitCounts.declinedTags = map[string]int{}
			}
			if emitCounts.declinedTags[tag]++; emitCounts.declinedTags[tag] <= 3 {
				fmt.Fprintf(os.Stderr, "tifa emit check: declined <%s> %s\n", tag, props)
			}
		case "differ":
			emitCounts.differ++
			fmt.Fprintf(os.Stderr, "tifa emit check: differ <%s> %s\n  C:     %q\n  React: %q\n", tag, props, c, js)
		default:
			return nil, fmt.Errorf("__tifa_emit_check: verdict %q", verdict)
		}
		if (emitCounts.same+emitCounts.declined+emitCounts.differ)%1000 == 1 {
			emitCounts.print()
		}
		return nil, nil
	})
}
