package compare_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
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
	page := func(line string) []string { return []string{"Thank you", line, "Back to the shop"} }
	if placedLine(page(en[0]), en) != 1 || placedLine(page(zh[0]), zh) != 1 {
		t.Fatal("the text the page writes was not found")
	}
	for name, c := range map[string]struct {
		line   string
		wanted []string
	}{
		"missing AM":           {"Placed on November 6, 2026 at 10:30", en},
		"24 hour in English":   {"Placed on November 6, 2026 at 18:30", en},
		"PM for AM":            {"Placed on November 6, 2026 at 10:30 PM", en},
		"UTC and not Pacific":  {placedTexts(t, "en-US", la.Add(8*time.Hour))[0], en},
		"8 hours off, Chinese": {placedTexts(t, "zh-CN", sh.Add(8*time.Hour))[0], zh},
		"the next day":         {placedTexts(t, "zh-CN", sh.Add(time.Minute))[0], zh},
		"without the message":  {"November 6, 2026 at 10:30 AM", en},
		"the other language":   {zh[0], en},
	} {
		if i := placedLine(page(c.line), c.wanted); i >= 0 {
			t.Errorf("%s: %q was taken for the time the order was placed", name, c.line)
		}
	}
}
