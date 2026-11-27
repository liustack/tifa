package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// GoRuntime holds the settings of the Go runtime that the .env file sets: GOMEMLIMIT, GOGC and
// GOMAXPROCS. Go reads these names from the environment when the program starts, before the
// program reads .env, so the program applies them itself. A field is nil when .env does not set
// the name, or when the environment does: the environment wins, and Go has applied it already.
type GoRuntime struct {
	MemoryLimit *int64 // GOMEMLIMIT, in bytes: math.MaxInt64 for off
	GCPercent   *int   // GOGC: -1 for off
	MaxProcs    *int   // GOMAXPROCS
}

// GoRuntime reads the settings of the Go runtime from .env, with the values Go takes for them. An
// invalid value is an error, where Go would stop the program or ignore it.
func (s *Sources) GoRuntime() (GoRuntime, error) {
	var g GoRuntime
	var errs []error
	value := func(name string) (string, bool) {
		if s.env[name] != "" || s.dotenv[name] == "" {
			return "", false
		}
		return s.dotenv[name], true
	}
	if v, ok := value("GOMEMLIMIT"); ok {
		n, err := goByteCount(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: .env GOMEMLIMIT: %q is not a byte count such as 150MiB, or off", v))
		}
		g.MemoryLimit = &n
	}
	if v, ok := value("GOGC"); ok {
		n, err := strconv.Atoi(v)
		switch {
		case v == "off":
			n = -1
		case err != nil || n < 0:
			errs = append(errs, fmt.Errorf("config: .env GOGC: %q is not a whole number such as 50, or off", v))
		}
		g.GCPercent = &n
	}
	if v, ok := value("GOMAXPROCS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("config: .env GOMAXPROCS: %q is not a whole number of at least 1", v))
		}
		g.MaxProcs = &n
	}
	if len(errs) > 0 {
		return GoRuntime{}, errors.Join(errs...)
	}
	return g, nil
}

// goByteCount reads a GOMEMLIMIT as Go does: a whole number of bytes with B, KiB, MiB, GiB or TiB
// or no unit, or off.
func goByteCount(v string) (int64, error) {
	if v == "off" {
		return math.MaxInt64, nil
	}
	units := []struct {
		suffix string
		shift  uint
	}{{"KiB", 10}, {"MiB", 20}, {"GiB", 30}, {"TiB", 40}, {"B", 0}}
	shift := uint(0)
	for _, u := range units {
		if strings.HasSuffix(v, u.suffix) {
			v, shift = strings.TrimSuffix(v, u.suffix), u.shift
			break
		}
	}
	n, err := strconv.ParseUint(v, 10, 63)
	if err != nil {
		return 0, err
	}
	if n > math.MaxInt64>>shift {
		return 0, strconv.ErrRange
	}
	return int64(n << shift), nil
}
