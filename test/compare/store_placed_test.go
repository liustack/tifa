package compare_test

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/test/compare"
)

// The confirmation of an order says when it was placed. The page writes it with the message of the
// language ("Placed on {{date}}") and Intl.DateTimeFormat(language, { dateStyle: 'long', timeStyle:
// 'short', timeZone }), the time zone being the language's. placedTexts writes the same text with
// Node's Intl for the time an order was placed, to be compared with the line of the page exactly.

var placedMessage = map[string]string{"en-US": "Placed on ", "zh-CN": "下单时间："}

var placedZone = map[string]string{"en-US": "America/Los_Angeles", "zh-CN": "Asia/Shanghai"}

const placedScript = `const [lang, zone, ...times] = process.argv.slice(1)
const message = lang === 'en-US' ? 'Placed on ' : '下单时间：'
for (const t of times) console.log(message + new Intl.DateTimeFormat(lang, { dateStyle: 'long', timeStyle: 'short', timeZone: zone }).format(new Date(Number(t))))`

// squash writes a text with its white space as one space, which is how a view keeps it (the short
// time of English has a no-break space before AM and PM).
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// placedTexts are the lines the confirmation in a language may show for an order that was placed at
// one of the times.
func placedTexts(t testing.TB, lang string, times ...time.Time) []string {
	t.Helper()
	args := []string{"-e", placedScript, lang, placedZone[lang]}
	for _, at := range times {
		args = append(args, strconv.FormatInt(at.UnixMilli(), 10))
	}
	out, err := exec.Command("node", args...).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var texts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		texts = append(texts, squash(line))
	}
	return texts
}

// placedLine finds the line of a page that says when the order was placed, which is the one text
// node of the page that is one of the wanted texts. It returns -1 for none or for more than one.
func placedLine(text []string, wanted []string) int {
	found := -1
	for i, line := range text {
		for _, w := range wanted {
			if line == w {
				if found >= 0 {
					return -1
				}
				found = i
				break // the same text for the two times is one line
			}
		}
	}
	return found
}

// normalizePlaced checks the confirmation page in view for an order placed between sent and done
// (the times of the request that placed it): exactly one line must be the text the page writes for
// one of those times, in the language and time zone of the page. That line is then replaced by
// "PLACED", so that the rest of the page can be compared as it is. checkPlacedTime of the storefront
// test calls it for both servers.
func normalizePlaced(t testing.TB, lang string, sent, done time.Time, view *compare.View) error {
	t.Helper()
	wanted := placedTexts(t, lang, sent, done)
	at := placedLine(view.Text, wanted)
	if at < 0 {
		var near []string
		for _, line := range view.Text {
			if strings.Contains(line, "Placed on") || strings.Contains(line, "下单时间") {
				near = append(near, line)
			}
		}
		return fmt.Errorf("no single line says %q, the order was placed at %s. Lines about it: %q", wanted, sent.Format(time.RFC3339Nano), near)
	}
	view.Text[at] = "PLACED"
	return nil
}

func TestPlacedTimeIsCheckedAsThePageWritesIt(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	// 10:30 in Los Angeles on 6 November 2026, and 23:59 in Shanghai on the 7th.
	la := time.Date(2026, 11, 6, 18, 30, 0, 0, time.UTC)
	sh := time.Date(2026, 11, 7, 15, 59, 30, 0, time.UTC)
	en := placedTexts(t, "en-US", la)
	zh := placedTexts(t, "zh-CN", sh)
	if en[0] != "Placed on November 6, 2026 at 10:30 AM" {
		t.Fatalf("the text of the English page is %q", en[0])
	}
	if !strings.HasPrefix(zh[0], "下单时间：2026年11月7日 23:59") {
		t.Fatalf("the text of the Chinese page is %q", zh[0])
	}
	page := func(line string) *compare.View {
		return &compare.View{Text: []string{"Thank you", line, "Back to the shop"}}
	}
	for lang, c := range map[string]struct {
		line string
		at   time.Time
	}{"en-US": {en[0], la}, "zh-CN": {zh[0], sh}} {
		v := page(c.line)
		if err := normalizePlaced(t, lang, c.at, c.at.Add(time.Second), v); err != nil || v.Text[1] != "PLACED" {
			t.Errorf("%s: the text the page writes was not accepted: %v, %q", lang, err, v.Text)
		}
	}
	for name, c := range map[string]struct {
		lang string
		line string
		at   time.Time
	}{
		"missing AM":           {"en-US", "Placed on November 6, 2026 at 10:30", la},
		"24 hour in English":   {"en-US", "Placed on November 6, 2026 at 18:30", la},
		"PM for AM":            {"en-US", "Placed on November 6, 2026 at 10:30 PM", la},
		"UTC and not Pacific":  {"en-US", placedTexts(t, "en-US", la.Add(8*time.Hour))[0], la},
		"8 hours off, Chinese": {"zh-CN", placedTexts(t, "zh-CN", sh.Add(8*time.Hour))[0], sh},
		"the next day":         {"zh-CN", placedTexts(t, "zh-CN", sh.Add(time.Minute))[0], sh},
		"without the message":  {"en-US", "November 6, 2026 at 10:30 AM", la},
		"the other language":   {"en-US", zh[0], la},
	} {
		v := page(c.line)
		if err := normalizePlaced(t, c.lang, c.at, c.at, v); err == nil {
			t.Errorf("%s: %q was taken for the time the order was placed", name, c.line)
		} else if v.Text[1] != c.line {
			t.Errorf("%s: a line that was refused was changed to %q", name, v.Text[1])
		}
	}
}
