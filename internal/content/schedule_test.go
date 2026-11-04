package content

import (
	"testing"
	"time"
)

func TestPublishedLine(t *testing.T) {
	d := time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, c := range []struct {
		shown []Shown
		want  string
	}{
		{[]Shown{{"blog", "en", "202611/release-notes", d}}, "blog en/202611/release-notes is published (date 2026-11-20 09:00 +08:00)"},
		{[]Shown{{"blog", "zh-CN", "202611/launch", d}, {"blog", "en", "202611/release-notes", d}}, "blog zh-CN/202611/launch and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)"},
		{[]Shown{{"blog", "en", "a", d}, {"docs", "", "b", d.Add(time.Minute)}}, "blog en/a and docs b are published (dates 2026-11-20 09:00 +08:00, 2026-11-20 09:01 +08:00)"},
	} {
		if got := publishedLine(c.shown); got != c.want {
			t.Errorf("publishedLine = %q\nwant %q", got, c.want)
		}
	}
}
