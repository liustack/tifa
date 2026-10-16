package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// value parses one setting from text, whatever layer the text came from.
type value interface {
	Set(string) error
	String() string
	kind() string
}

type stringValue struct{ p *string }

func (v stringValue) Set(s string) error {
	if s == "" {
		return errors.New("must not be empty")
	}
	*v.p = s
	return nil
}
func (v stringValue) String() string { return *v.p }
func (v stringValue) kind() string   { return "string" }

type intValue struct {
	p   *int
	min int
}

func (v intValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid integer %q", s)
	}
	if n < v.min {
		return fmt.Errorf("%d is below the minimum %d", n, v.min)
	}
	*v.p = n
	return nil
}
func (v intValue) String() string { return strconv.Itoa(*v.p) }
func (v intValue) kind() string   { return "int" }

type boolValue struct{ p *bool }

func (v boolValue) Set(s string) error {
	b, err := strconv.ParseBool(s) // also True and TRUE, which YAML reads as bools
	if err != nil {
		return fmt.Errorf("invalid bool %q, use true or false", s)
	}
	*v.p = b
	return nil
}
func (v boolValue) String() string { return strconv.FormatBool(*v.p) }
func (v boolValue) kind() string   { return "bool" }

type durationValue struct{ p *time.Duration }

func (v durationValue) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid duration %q, use a positive value such as 3s or 500ms", s)
	}
	*v.p = d
	return nil
}
func (v durationValue) String() string { return v.p.String() }
func (v durationValue) kind() string   { return "duration" }

var sizeUnits = []struct {
	name string
	n    int64
}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}

// sizeValue is a byte count written as a whole number with an optional B, KiB, MiB or GiB unit.
type sizeValue struct {
	p   *int64
	min int64
}

func (v sizeValue) Set(s string) error {
	digits, unit := s, int64(1)
	for _, u := range sizeUnits {
		if rest, ok := strings.CutSuffix(s, u.name); ok {
			digits, unit = rest, u.n
			break
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/unit {
		return fmt.Errorf("invalid size %q, use a whole number with B, KiB, MiB or GiB", s)
	}
	if n*unit < v.min {
		return fmt.Errorf("size %q is below the minimum %d bytes", s, v.min)
	}
	*v.p = n * unit
	return nil
}

func (v sizeValue) String() string {
	for _, u := range sizeUnits {
		if *v.p != 0 && *v.p%u.n == 0 {
			return strconv.FormatInt(*v.p/u.n, 10) + u.name
		}
	}
	return "0"
}
func (v sizeValue) kind() string { return "size" }

// rawValue keeps the flag text so that parsing errors carry the layer and key like every other layer.
type rawValue struct {
	s   string
	typ string
}

func (v *rawValue) Set(s string) error { v.s = s; return nil }
func (v *rawValue) String() string     { return v.s }
func (v *rawValue) Type() string       { return v.typ }
