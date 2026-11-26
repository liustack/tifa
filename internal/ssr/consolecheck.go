package ssr

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"

	"github.com/liustack/beastjs"
	"github.com/liustack/tifa/internal/core"
)

// takeConsole is what returns, and forgets, the console calls consolecheck.js wrote down.
const takeConsole = "globalThis.__tifa_console_take__()"

// consoleCall is a call of console.log, info, debug or warn that consolecheck.js wrote down: the
// level, the text of the arguments and the stack of the call.
type consoleCall struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	Stack string `json:"stack"`
}

type consoleCalls struct {
	Calls   []consoleCall `json:"calls"`
	Dropped int           `json:"dropped"`
}

// logConsole takes the console calls of the render vm just ran, which leaves the list empty for
// the next one, and logs each with the URL of the page and the place of the call in the sources
// of the app, when the stack has one. A render for the clock check of tifa dev (see WithReads) is
// the second render of a page that logged already, so its calls are dropped.
func (p *Pool) logConsole(ctx context.Context, vm *beastjs.VM, url string) error {
	if err := vm.SetEvalTimeout(p.timeout); err != nil {
		return err
	}
	value, err := vm.Eval(takeConsole, beastjs.EvalGlobal)
	if err != nil {
		return err
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("got %T, want a JSON string", value)
	}
	if ctx.Value(readsKey{}) != nil {
		return nil
	}
	var calls consoleCalls
	if err := json.Unmarshal([]byte(text), &calls, fromVM); err != nil {
		return err
	}
	for _, c := range calls.Calls {
		level := slog.LevelInfo
		if c.Level == "warn" {
			level = slog.LevelWarn
		}
		attrs := []any{"path", url}
		if place, ok := p.sources.appPlace(c.Stack); ok {
			attrs = append(attrs, "at", place)
		}
		core.Logger().Log(ctx, level, "console."+c.Level+": "+c.Text, attrs...)
	}
	if calls.Dropped > 0 {
		core.Logger().Info(fmt.Sprintf("console: %d more console calls not shown", calls.Dropped), "path", url)
	}
	return nil
}
