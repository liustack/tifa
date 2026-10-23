package ssr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liustack/slowjs"
)

// intlWarning is a call intlcheck.js wrote down: what was called, whether it had no locale and
// no time zone, and the stack of the call, its places in the bundle.
type intlWarning struct {
	Call   string `json:"call"`
	Locale bool   `json:"locale"`
	Zone   bool   `json:"zone"`
	Stack  string `json:"stack"`
}

// intlAdvice says why a call that uses the defaults of Intl is a problem and what to do.
const intlAdvice = "The server formats in en-US and UTC, a browser in the language and time zone of its user, " +
	"so the text can differ from the HTML and React reports a hydration mismatch. " +
	"Pass the locale and, for a date, the timeZone, from data Load returns, " +
	"as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect."

// warnIntl logs the Intl calls vm wrote down since it was last asked, each place once for the
// pool, with the places of its stack in the sources.
func (p *Pool) warnIntl(vm *slowjs.VM) error {
	if err := vm.SetEvalTimeout(p.timeout); err != nil {
		return err
	}
	value, err := vm.Eval("globalThis.__tifa_intl_warnings__()", slowjs.EvalGlobal)
	if err != nil {
		return err
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("got %T, want a JSON string", value)
	}
	var warnings []intlWarning
	if err := json.Unmarshal([]byte(text), &warnings); err != nil {
		return err
	}
	for _, w := range warnings {
		missing := "a locale and a timeZone"
		switch {
		case !w.Zone:
			missing = "a locale"
		case !w.Locale:
			missing = "a timeZone"
		}
		place := p.firstPlace(w.Stack)
		key := w.Call + " " + missing + " " + place
		p.mu.Lock()
		seen := p.warned[key]
		p.warned[key] = true
		p.mu.Unlock()
		if seen {
			continue
		}
		stack := bundlePlace.ReplaceAllStringFunc(w.Stack, func(place string) string {
			line, column := placeOf(place)
			return p.sources.name(line, column)
		})
		logf("tifa: ssr: %s without %s at %s. %s\n%s", w.Call, missing, place, intlAdvice, stack)
	}
	return nil
}

// firstPlace names the place of the first frame of stack, in the sources when it is in the
// bundle.
func (p *Pool) firstPlace(stack string) string {
	frame, _, _ := strings.Cut(stack, "\n")
	if place := bundlePlace.FindString(frame); place != "" {
		line, column := placeOf(place)
		return p.sources.name(line, column)
	}
	frame = strings.TrimSpace(frame)
	if i := strings.LastIndexByte(frame, '('); i >= 0 {
		return strings.TrimSuffix(frame[i+1:], ")")
	}
	if frame == "" {
		return "an unknown place"
	}
	return strings.TrimPrefix(frame, "at ")
}
