package compare

import (
	"math"
	"slices"
)

// Median is the middle of the values, or the mean of the two in the middle. It is 0 for none.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := slices.Clone(values)
	slices.Sort(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Range is the smallest and the largest of the values, 0 and 0 for none.
func Range(values []float64) (lo, hi float64) {
	if len(values) == 0 {
		return 0, 0
	}
	return slices.Min(values), slices.Max(values)
}

// Percentile is the nearest-rank percentile (p from 0 to 100) of values sorted ascending.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[min(max(rank, 1), len(sorted))-1]
}

// Rotate returns the items starting from item round%len(items) and wrapping around, so that
// round after round each item starts the order in turn. round counts from 0.
func Rotate[T any](items []T, round int) []T {
	if len(items) == 0 {
		return nil
	}
	k := round % len(items)
	return append(slices.Clone(items[k:]), items[:k]...)
}

const (
	settleWindow = 30
	settleWithin = 0.10
)

// Settle returns the request, counting from 1, from which latency has settled: the first one
// after which the median of every window of 30 requests stays within 10% of the steady latency
// of its pages. latencies are in order, and request i asks for page i%len(steady), whose steady
// latency is steady[i%len(steady)]. It returns 0 when the last window is not within 10 percent.
func Settle(latencies, steady []float64) int {
	n := len(latencies) - settleWindow + 1
	if n < 1 || len(steady) == 0 {
		return 0
	}
	ratios := make([]float64, len(latencies))
	for i, l := range latencies {
		ratios[i] = l / steady[i%len(steady)]
	}
	settled := n // index of the first window of the final run of good windows
	for k := n - 1; k >= 0; k-- {
		if math.Abs(Median(ratios[k:k+settleWindow])-1) > settleWithin {
			break
		}
		settled = k
	}
	if settled == n {
		return 0
	}
	return settled + 1
}

func sortedCopy(v []float64) []float64 {
	out := slices.Clone(v)
	slices.Sort(out)
	return out
}
