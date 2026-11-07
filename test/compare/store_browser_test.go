package compare_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Next.js does not write the not-found page of a storefront language into the HTML it answers
// 404 with. The browser draws it from the payload of the page. TestStorefront compares the
// status of those pages, and this test opens them in a browser (playwright-cli, headed, one
// stateless session) and compares what the visitor ends up reading with Tifa's page.

var errorPages = []string{
	"/store/products/nothing",
	"/zh-CN/store/xx-XX",
	"/zh-CN/store/checkout?placed=SS-NOSUCHORDER",
	"/zh-CN/store/journal/nothing",
	"/store/no-such-page",
}

// browser drives one playwright-cli session.
type browser struct {
	t       *testing.T
	session string
	opened  bool
}

func (b *browser) run(args ...string) string {
	b.t.Helper()
	out, err := exec.Command("playwright-cli", append([]string{"-s=" + b.session}, args...)...).CombinedOutput()
	if err != nil {
		b.t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// read opens url and returns the title and the text of the page once it stops changing.
func (b *browser) read(url string) (title, text string) {
	b.t.Helper()
	if !b.opened {
		b.run("open", url, "--headed")
		b.opened = true
	} else {
		b.run("goto", url)
	}
	var last string
	for i := 0; i < 20; i++ {
		time.Sleep(300 * time.Millisecond)
		out := b.run("eval", "() => JSON.stringify({title: document.title, text: document.body.innerText})")
		_, rest, ok := strings.Cut(out, "### Result\n")
		if !ok {
			b.t.Fatalf("no result in %s", out)
		}
		line, _, _ := strings.Cut(rest, "\n")
		unquoted, err := strconv.Unquote(line)
		if err != nil {
			b.t.Fatalf("result %q: %v", line, err)
		}
		if unquoted == last {
			break
		}
		last = unquoted
	}
	var page struct{ Title, Text string }
	if err := json.Unmarshal([]byte(last), &page); err != nil {
		b.t.Fatal(err)
	}
	return page.Title, strings.Join(strings.Fields(page.Text), " ")
}

func TestStorefrontErrorPagesInTheBrowser(t *testing.T) {
	env := setup(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	buildApps(t, env)
	ctx := context.Background()
	tifa, err := mustTarget(t, env, "tifa-nocache").Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer tifa.Stop()
	next, err := mustTarget(t, env, "nextjs").Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Stop()

	b := &browser{t: t, session: "storefront-errors"}
	defer func() {
		if b.opened {
			b.run("close")
		}
	}()
	for _, path := range errorPages {
		wantTitle, wantText := b.read(tifa.Base + path)
		title, text := b.read(next.Base + path)
		if wantText == "" || !strings.Contains(wantText, "Shanhai") && !strings.Contains(wantText, "山海") {
			t.Errorf("%s: Tifa's page has no text: %q", path, wantText)
		}
		if title != wantTitle {
			t.Errorf("%s: title %q in the browser against Tifa's %q", path, title, wantTitle)
		}
		if text != wantText {
			t.Errorf("%s: the page reads differently.\nTifa:    %s\nNext.js: %s", path, wantText, text)
		}
	}
}
