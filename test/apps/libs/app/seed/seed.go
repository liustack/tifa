// Package seed makes the same fixed values as a small linear congruential generator, so the
// pages of this app always load the same data.
package seed

import (
	"math"
	"strings"
)

var words = []string{"amber", "basalt", "cedar", "delta", "ember", "fjord", "garnet", "harbor", "indigo", "juniper",
	"kelp", "lumen", "marble", "nectar", "onyx", "pepper", "quartz", "russet", "sable", "tundra", "umber", "velvet",
	"willow", "xenon", "yarrow", "zephyr", "Café", "naïve", "Ünïcode", "日本", "R&D", "<b>", "\"q\""}

// Rand is a generator with a fixed start.
type Rand struct{ s uint32 }

func New(seed uint32) *Rand { return &Rand{s: seed} }

// R returns a number in [0, 1).
func (r *Rand) R() float64 {
	r.s = r.s*1664525 + 1013904223
	return float64(r.s) / 4294967296
}

// Int returns a whole number in [0, n).
func (r *Rand) Int(n int) int { return int(math.Floor(r.R() * float64(n))) }

// Pick returns one of the values.
func Pick[T any](r *Rand, a []T) T { return a[r.Int(len(a))] }

// Words returns n words from the fixed list, joined by spaces.
func (r *Rand) Words(n int) string {
	o := make([]string, n)
	for i := range o {
		o[i] = Pick(r, words)
	}
	return strings.Join(o, " ")
}
