package config

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// AppSetting is one setting the app defines in its config.go, such as db.url.
type AppSetting struct {
	Key, Help string
	// Kind is the type of the value: string, strings, int, bool, duration or size.
	Kind string
	// Value is the value as text, and From where it came from, empty for the default.
	Value, From string
}

// registry holds the settings of the app, in the order they were defined.
type registry struct {
	mu       sync.Mutex
	settings []AppSetting
	errs     []error
}

var app registry

// AppSettings returns the settings of the app with their values, in the order of config.go.
func AppSettings() []AppSetting {
	app.mu.Lock()
	defer app.mu.Unlock()
	return append([]AppSetting(nil), app.settings...)
}

// AppErr reports the invalid values the settings of the app were given.
func AppErr() error {
	app.mu.Lock()
	defer app.mu.Unlock()
	return errors.Join(app.errs...)
}

// define registers the setting key of the app, whose value v holds the default, and sets v from
// s. It returns false when a source gave an invalid value, which it records. A key that is not
// valid, or that clashes with another one, is a mistake in the code, which tifa build reports,
// so it panics. s is nil in a test binary, where settings keep their defaults.
func (r *registry) define(s *Sources, key, help string, v value) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := CheckKey(key); err != nil {
		panic("tifa: " + err.Error())
	}
	var others []string
	for _, o := range r.settings {
		others = append(others, o.Key)
	}
	if err := Clash(key, others); err != nil {
		panic("tifa: " + err.Error())
	}
	def := v.String()
	from := ""
	var errs []error
	if s != nil {
		from, errs = s.apply(key, v, nil)
	}
	ok := len(errs) == 0
	if !ok {
		r.errs = append(r.errs, errs...)
		if err := v.Set(def); err != nil {
			panic(fmt.Sprintf("tifa: setting %s: the default does not read back: %v", key, err))
		}
		from = ""
	}
	r.settings = append(r.settings, AppSetting{Key: key, Help: help, Kind: v.kind(), Value: v.String(), From: from})
	return ok
}

// appSources is where the settings of the app come from: nothing in a test binary, whose
// arguments and working directory are those of go test.
func appSources() *Sources {
	if testing.Testing() {
		return nil
	}
	return Process()
}

// DefineString is tifa.String.
func DefineString(key, def, help string) string {
	v := def
	if !app.define(appSources(), key, help, optionalValue{&v}) {
		return def
	}
	return v
}

// DefineStrings is tifa.Strings.
func DefineStrings(key string, def []string, help string) []string {
	v := append([]string(nil), def...)
	if !app.define(appSources(), key, help, listValue{&v}) {
		return def
	}
	return v
}

// DefineInt is tifa.Int.
func DefineInt(key string, def int, help string) int {
	v := def
	if !app.define(appSources(), key, help, intValue{p: &v, min: math.MinInt}) {
		return def
	}
	return v
}

// DefineBool is tifa.Bool.
func DefineBool(key string, def bool, help string) bool {
	v := def
	if !app.define(appSources(), key, help, boolValue{&v}) {
		return def
	}
	return v
}

// DefineDuration is tifa.Duration.
func DefineDuration(key string, def time.Duration, help string) time.Duration {
	v := def
	if !app.define(appSources(), key, help, anyDurationValue{&v}) {
		return def
	}
	return v
}

// DefineSize is tifa.Size.
func DefineSize(key string, def int64, help string) int64 {
	v := def
	if def < 0 {
		panic(fmt.Sprintf("tifa: setting %s: a size is not negative", key))
	}
	if !app.define(appSources(), key, help, sizeValue{&v, 0}) {
		return def
	}
	return v
}

// listValue is a comma-separated list, split as the lists of tifa are, empty for none.
type listValue struct{ p *[]string }

func (v listValue) Set(s string) error {
	items, err := splitList(s)
	if err != nil {
		return err
	}
	*v.p = items
	return nil
}
func (v listValue) String() string { return strings.Join(*v.p, ",") }
func (v listValue) kind() string   { return "strings" }

// anyDurationValue is a duration of the app, in Go's format, of any sign.
type anyDurationValue struct{ p *time.Duration }

func (v anyDurationValue) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q, use a value such as 3s or 500ms", s)
	}
	*v.p = d
	return nil
}
func (v anyDurationValue) String() string { return v.p.String() }
func (v anyDurationValue) kind() string   { return "duration" }
