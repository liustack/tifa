package compare

import (
	"math"
	"time"
)

// SeriesLength is the length of a step of the time series of a round.
const SeriesLength = 10 * time.Second

// SeriesStep is what a round did in one step of its time series: the page views that completed in the
// ten seconds from From (seconds after the first request), their rate and the P99 of their latency.
// The last step is as long as the round is, not ten seconds, and its rate is over that.
type SeriesStep struct {
	From      int     `json:"fromSecond"`
	PageViews int     `json:"pageViews"`
	PerSecond float64 `json:"pageViewsPerSecond"`
	P99Ms     float64 `json:"p99Ms"`
}

// viewSample is one page view: when it completed and its latency in ms.
type viewSample struct {
	at time.Time
	ms float64
}

// viewSeries puts the page views of a round, which started at start and ended at end, into steps of
// SeriesLength. A step with no page view is there, with 0: a stall shows.
func viewSeries(views []viewSample, start, end time.Time) []SeriesStep {
	total := end.Sub(start)
	if total <= 0 {
		return nil
	}
	n := int(math.Ceil(float64(total) / float64(SeriesLength)))
	lat := make([][]float64, n)
	for _, v := range views {
		i := min(max(int(v.at.Sub(start)/SeriesLength), 0), n-1)
		lat[i] = append(lat[i], v.ms)
	}
	out := make([]SeriesStep, n)
	for i := range out {
		length := min(SeriesLength, total-time.Duration(i)*SeriesLength)
		out[i] = SeriesStep{From: i * int(SeriesLength/time.Second), PageViews: len(lat[i]), PerSecond: float64(len(lat[i])) / length.Seconds()}
		if len(lat[i]) > 0 {
			out[i].P99Ms = Percentile(sortedCopy(lat[i]), 99)
		}
	}
	return out
}
