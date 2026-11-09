package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

var reactText = regexp.MustCompile(`<!-- -->`)

// plain is the body without the markers React writes between texts.
func plain(body string) string { return reactText.ReplaceAllString(body, "") }

// TestI18nApp builds test/apps/i18n, a store in zh-CN and en-US next to a page and an API route
// outside every area, and checks from outside what the server does with the language of a URL:
// the 308s and 404s, the language and the URL the code of the app sees, the render memo and
// the ETag that never cross languages, and tifa.Languages.
func TestI18nApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "i18n")
	typecheck(t, dir)
	base, _, _ := start(t, exe)
	id := buildID(t, dir)
	data := func(path, digits string) string { return base + path + sep(path) + "_tifa=" + id + "." + digits }

	type mine struct{ Language, URL string }
	readMine := func(t *testing.T, target string) mine {
		t.Helper()
		status, _, body := cacheSend(t, "GET", base+target, "")
		var m mine
		if status != 200 || json.Unmarshal([]byte(body), &m) != nil {
			t.Fatalf("GET %s: %d %s", target, status, body)
		}
		return m
	}

	t.Run("the language of a route and the URL its code sees", func(t *testing.T) {
		if got := readMine(t, "/store/mine"); got != (mine{"zh-CN", "/store/mine"}) {
			t.Errorf("default language: %+v", got)
		}
		// The query stays, in the order and spelling it came in.
		if got := readMine(t, "/en-US/store/mine?x=1&y=%C3%A9&x=2"); got != (mine{"en-US", "/en-US/store/mine?x=1&y=%C3%A9&x=2"}) {
			t.Errorf("en-US: %+v", got)
		}
		// A route outside every area has the default language and no prefix.
		_, _, body := cacheSend(t, "GET", base+"/api/hook", "")
		if body != `{"language":"zh-CN"}`+"\n" && body != `{"language":"zh-CN"}` {
			t.Errorf("/api/hook: %s", body)
		}
		if status, _, body := cacheSend(t, "GET", base+"/projects", ""); status != 200 || !strings.Contains(plain(body), "projects go=zh-CN react=zh-CN") {
			t.Errorf("/projects: %d %.300s", status, body)
		}
		if status, _, body := cacheSend(t, "GET", base+"/store?q=a", ""); status != 200 || !strings.Contains(plain(body), "store go=zh-CN react=zh-CN path=/store q=a title=商店") {
			t.Errorf("/store: %d %.300s", status, body)
		}
	})

	t.Run("a prefix the route has no language for is a 404", func(t *testing.T) {
		for _, path := range []string{"/en-US/projects", "/en-US/api/hook", "/en-US/favicon.svg", "/zh-CN/api/hook", "/fr/store/mine", "/EN-US/store/mine"} {
			if status, _, _ := cacheSend(t, "GET", base+path, ""); status != 404 {
				t.Errorf("GET %s: %d, want 404", path, status)
			}
		}
		// The public file itself is there.
		if status, _, _ := cacheSend(t, "GET", base+"/favicon.svg", ""); status != 200 {
			t.Errorf("GET /favicon.svg: %d", status)
		}
		for _, path := range []string{"/store/zzz", "/en-US/zzz"} {
			if status, _, body := cacheSend(t, "GET", base+path, ""); status != 404 || !strings.Contains(body, "Page not found") {
				t.Errorf("GET %s: %d %.200s", path, status, body)
			}
		}
		if status, _, body := cacheSend(t, "GET", base+"/en-US/api/hook", ""); status != 404 || !strings.Contains(body, "Not Found") {
			t.Errorf("GET /en-US/api/hook: %d %s", status, body)
		}
	})

	t.Run("the default language with a prefix is a 308 that keeps the query", func(t *testing.T) {
		for _, tc := range []struct{ path, location string }{
			{"/zh-CN/store?x=1&q=a", "/store?x=1&q=a"},
			{"/zh-CN/store/mine", "/store/mine"},
			{"/zh-CN/store/news?utm_source=a", "/store/news?utm_source=a"},
			// ServeMux's own redirects keep the prefix.
			{"/en-US//store/mine?x=1", "/en-US/store/mine?x=1"},
			{"//store/mine?x=1", "/store/mine?x=1"},
		} {
			status, h, _ := cacheSend(t, "GET", base+tc.path, "")
			if status != 308 || h.Get("Location") != tc.location {
				t.Errorf("GET %s: %d Location %q, want 308 %q", tc.path, status, h.Get("Location"), tc.location)
			}
		}
		// Another spelling of the prefix is the spelling ServeMux makes canonical, and the prefix after it.
		if status, h, _ := cacheSend(t, "GET", base+"/en%2DUS/store/mine", ""); status != 308 || h.Get("Location") != "/en-US/store/mine" {
			t.Errorf("GET /en%%2DUS/store/mine: %d Location %q", status, h.Get("Location"))
		}
		// A data request of the browser gets the redirect as data, with the query it asked with.
		status, _, body := cacheSend(t, "GET", data("/zh-CN/store?q=a", "1"), "")
		if status != 200 || !strings.Contains(body, `"redirect":"/store?q=a"`) || !strings.Contains(body, `"status":308`) {
			t.Errorf("data request of /zh-CN/store: %d %s", status, body)
		}
	})

	t.Run("the render memo and the ETag never cross languages", func(t *testing.T) {
		news := func(path string) (string, string, string) {
			t.Helper()
			status, h, body := cacheSend(t, "GET", base+path, "")
			if status != 200 {
				t.Fatalf("GET %s: %d %.300s", path, status, body)
			}
			return h.Get("ETag"), h.Get("X-Tifa-Render"), body
		}
		zhTag, zhHow, zhBody := news("/store/news")
		enTag, enHow, enBody := news("/en-US/store/news")
		// The page is the news page of its own language, not a 404 with a status of 200.
		for body, language := range map[string]string{zhBody: "zh-CN", enBody: "en-US"} {
			if text := plain(body); !strings.Contains(text, "react="+language) || strings.Contains(text, "Page not found") {
				t.Errorf("the first %s page: %.1500s", language, text)
			}
		}
		if zhHow != "vm" || enHow != "vm" {
			t.Errorf("first requests: %q and %q, each language renders its own page", zhHow, enHow)
		}
		if zhTag == "" || zhTag == enTag {
			t.Errorf("ETags %q and %q, want two", zhTag, enTag)
		}
		if _, status, body := news("/store/news"); status != "memo" || body != zhBody {
			t.Errorf("second zh-CN request: %q", status)
		}
		if _, status, body := news("/en-US/store/news"); status != "memo" || body != enBody {
			t.Errorf("second en-US request: %q", status)
		}
		// The tag of one language does not answer 304 for the other.
		if status, _, _ := cacheSend(t, "GET", base+"/en-US/store/news", "", "If-None-Match", zhTag); status == 304 {
			t.Errorf("the ETag of zh-CN answered 304 for en-US")
		}
		if status, _, _ := cacheSend(t, "GET", base+"/store/news", "", "If-None-Match", zhTag); status != 304 {
			t.Errorf("the ETag of zh-CN answered %d for zh-CN", status)
		}
		// The data is the same in both languages, and each language still made its own page.
		_, _, zhData := cacheSend(t, "GET", data("/store/news", "1"), "")
		_, _, enData := cacheSend(t, "GET", data("/en-US/store/news", "1"), "")
		const same = `"data":{"body":"the same in every language"}`
		if !strings.Contains(zhData, same) || !strings.Contains(enData, same) {
			t.Errorf("data of zh-CN %s and of en-US %s", zhData, enData)
		}
		// The page of the render memo carries the language of its own render.
		// (The list of versions names en-US too, so the language of the page is the one in front of it.)
		if !regexp.MustCompile(`dehydratedData:\$R\[\d+\]=\{buildId:"[^"]*",language:"zh-CN"`).MatchString(zhBody) {
			t.Errorf("the hydration data of the zh-CN page: %.200s", zhBody)
		}
	})

	t.Run("a page in each language renders with its own prefix on its links", func(t *testing.T) {
		for _, tc := range []struct{ path, text, cart string }{
			{"/store", "store go=zh-CN react=zh-CN path=/store", `href="/store/cart"`},
			{"/en-US/store", "store go=en-US react=en-US path=/store", `href="/en-US/store/cart"`},
			{"/en-US/store?q=a", "store go=en-US react=en-US path=/store q=a", `href="/en-US/store/cart"`},
			{"/en-US/store/cart", "cart go=en-US react=en-US", `href="/en-US/store"`},
			{"/store/cart", "cart go=zh-CN react=zh-CN", `href="/store"`},
		} {
			status, _, body := cacheSend(t, "GET", base+tc.path, "")
			if status != 200 || !strings.Contains(plain(body), tc.text) || !strings.Contains(body, tc.cart) || strings.Contains(body, "Page not found") {
				t.Errorf("GET %s: %d %.500s", tc.path, status, plain(body))
			}
			// A link to a page outside every area has no prefix, in any language.
			if tc.path != "/store/cart" && tc.path != "/en-US/store/cart" && !strings.Contains(body, `href="/projects"`) {
				t.Errorf("GET %s: the link to /projects has a prefix", tc.path)
			}
		}
	})

	t.Run("a page without SSR carries its language, and its data request has the prefix", func(t *testing.T) {
		for path, language := range map[string]string{"/store/csr": "zh-CN", "/en-US/store/csr": "en-US"} {
			status, _, body := cacheSend(t, "GET", base+path, "")
			if status != 200 || !strings.Contains(body, `"language":"`+language+`"`) || !strings.Contains(body, `"url":"`+path+`"`) || strings.Contains(body, "Page not found") {
				t.Errorf("GET %s: %d %.600s", path, status, body)
			}
		}
	})

	t.Run("the query of a data request reaches the Load", func(t *testing.T) {
		_, _, body := cacheSend(t, "GET", data("/en-US/store?q=a", "1"), "")
		if !strings.Contains(body, `"q":"a"`) || !strings.Contains(body, `"language":"en-US"`) {
			t.Errorf("%s", body)
		}
	})

	// headOf returns what is between <head> and </head> of the page at path, and the start of the page.
	headOf := func(t *testing.T, path string, want int) (head, start string) {
		t.Helper()
		status, _, body := cacheSend(t, "GET", base+path, "")
		if status != want {
			t.Fatalf("GET %s: %d, want %d: %.300s", path, status, want, body)
		}
		i, j := strings.Index(body, "<head>"), strings.Index(body, "</head>")
		if i < 0 || j < i {
			t.Fatalf("GET %s has no head: %.300s", path, body)
		}
		return body[i:j], body[:i]
	}
	const site = "https://i18n.example.com"

	t.Run("the language of the page is the lang of <html>", func(t *testing.T) {
		for _, tc := range []struct {
			path   string
			status int
			lang   string
		}{
			{"/store?q=a", 200, "zh-CN"},
			// A route outside every area has the default language, and so does a page that is not found.
			{"/projects", 200, "zh-CN"},
			{"/store/zzz", 404, "zh-CN"},
			{"/en-US/zzz", 404, "en-US"},
		} {
			if _, start := headOf(t, tc.path, tc.status); start != `<!DOCTYPE html><html lang="`+tc.lang+`">` {
				t.Errorf("GET %s starts with %s, want lang %s", tc.path, start, tc.lang)
			}
		}
	})

	t.Run("a page with more than one version links to each of them", func(t *testing.T) {
		head, _ := headOf(t, "/store?q=a", 200)
		// The links come in the order of the languages, x-default after them and the page itself last,
		// as full URLs from site.url and without the query.
		want := `<link rel="alternate" hreflang="zh-CN" href="` + site + `/store">` +
			`<link rel="alternate" hreflang="en-US" href="` + site + `/en-US/store">` +
			`<link rel="alternate" hreflang="x-default" href="` + site + `/store">` +
			`<link rel="canonical" href="` + site + `/store">`
		if !strings.Contains(head, want) {
			t.Errorf("the head of /store lacks the links:\n%s", head)
		}
		// The links of a translated slug and of a missing translation come from SetAlternates.
		head, _ = headOf(t, "/store/journal/ni-hao", 200)
		for _, link := range []string{
			`<link rel="alternate" hreflang="zh-CN" href="` + site + `/store/journal/ni-hao">`,
			`<link rel="alternate" hreflang="en-US" href="` + site + `/en-US/store/journal/hello">`,
			`<link rel="alternate" hreflang="x-default" href="` + site + `/store/journal/ni-hao">`,
			`<link rel="canonical" href="` + site + `/store/journal/ni-hao">`,
		} {
			if !strings.Contains(head, link) {
				t.Errorf("the head of /store/journal/ni-hao lacks %s:\n%s", link, head)
			}
		}
		// The language switch reads the same list.
		if _, _, body := cacheSend(t, "GET", base+"/store/journal/ni-hao", ""); !strings.Contains(plain(body), `<a href="/en-US/store/journal/hello">en-US</a>`) {
			t.Errorf("the switch of /store/journal/ni-hao: %.600s", body)
		}
		// A page with a prefix has the same links, and is its own canonical URL.
		for path, links := range map[string][]string{
			"/en-US/store?q=a": {
				`<link rel="alternate" hreflang="zh-CN" href="` + site + `/store">` +
					`<link rel="alternate" hreflang="en-US" href="` + site + `/en-US/store">` +
					`<link rel="alternate" hreflang="x-default" href="` + site + `/store">` +
					`<link rel="canonical" href="` + site + `/en-US/store">`,
			},
			"/en-US/store/journal/hello": {
				`<link rel="alternate" hreflang="zh-CN" href="` + site + `/store/journal/ni-hao">`,
				`<link rel="alternate" hreflang="en-US" href="` + site + `/en-US/store/journal/hello">`,
				`<link rel="canonical" href="` + site + `/en-US/store/journal/hello">`,
			},
		} {
			head, _ := headOf(t, path, 200)
			for _, link := range links {
				if !strings.Contains(head, link) {
					t.Errorf("the head of %s lacks %s:\n%s", path, link, head)
				}
			}
		}
		if _, _, body := cacheSend(t, "GET", base+"/en-US/store/journal/hello", ""); !strings.Contains(plain(body), `<a href="/store/journal/ni-hao">zh-CN</a>`) {
			t.Errorf("the switch of /en-US/store/journal/hello: %.600s", body)
		}
		// An entry in one language has one version, and a page outside every area, a page that is not
		// found and one that is not an entry have none.
		for path, status := range map[string]int{"/store/journal/only-zh": 200, "/projects": 200, "/store/zzz": 404, "/store/journal/nope": 404} {
			if head, _ := headOf(t, path, status); strings.Contains(head, "hreflang") || strings.Contains(head, `rel="canonical"`) {
				t.Errorf("the head of %s has links:\n%s", path, head)
			}
		}
		// A canonical the app writes wins, the links of the versions stay.
		head, _ = headOf(t, "/store/news", 200)
		if strings.Count(head, `rel="canonical"`) != 1 || !strings.Contains(head, `<link rel="canonical" href="`+site+`/store/news-own">`) || !strings.Contains(head, `hreflang="en-US" href="`+site+`/en-US/store/news"`) {
			t.Errorf("the head of /store/news:\n%s", head)
		}
	})

	t.Run("the data of a navigation carries only the lists the app set", func(t *testing.T) {
		_, _, body := cacheSend(t, "GET", data("/en-US/store/journal/hello", "1"), "")
		want := `"alternates":{"list":[{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}]}`
		if !strings.Contains(body, want) {
			t.Errorf("%s", body)
		}
		// An entry in one language has a list of one. The other pages of the area, asked for every
		// Load, say {}: the browser works out their versions from the table of the build. A page
		// outside every area says nothing.
		_, _, body = cacheSend(t, "GET", data("/store/journal/only-zh", "1"), "")
		if !strings.Contains(body, `"alternates":{"list":[{"language":"zh-CN","url":"/store/journal/only-zh"}]}`) {
			t.Errorf("%s", body)
		}
		for path, want := range map[string]bool{"/store?q=a": true, "/en-US/store/news": true, "/projects": false} {
			if _, _, body := cacheSend(t, "GET", data(path, "1"), ""); strings.Contains(body, `"alternates":{}`) != want || !want && strings.Contains(body, "alternates") {
				t.Errorf("%s: %s", path, body)
			}
		}
	})

	t.Run("the page carries the site and the default language, and the list only when the app set it", func(t *testing.T) {
		_, _, body := cacheSend(t, "GET", base+"/store", "")
		if !strings.Contains(body, `alternates:$R[`) || !strings.Contains(body, `site:"`+site+`"`) || strings.Contains(body, "canonical:") {
			t.Errorf("/store: %.1500s", body)
		}
		_, _, body = cacheSend(t, "GET", base+"/store/journal/ni-hao", "")
		if !strings.Contains(body, `url:"/en-US/store/journal/hello"`) {
			t.Errorf("/store/journal/ni-hao: %.1500s", body)
		}
	})

	t.Run("tifa.Languages at init", func(t *testing.T) {
		_, _, body := cacheSend(t, "GET", base+"/api/languages", "")
		if !strings.Contains(body, `"atInit":["zh-CN","en-US"]`) || !strings.Contains(body, `"now":["zh-CN","en-US"]`) {
			t.Errorf("%s", body)
		}
	})

	t.Run("go test in a folder of the app reads the files", func(t *testing.T) {
		cmd := exec.Command("go", "test", "./app/api/languages")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test: %v\n%s", err, out)
		}
	})
}

// tifa.Languages is the same under tifa build with the -ldflags of GOFLAGS and with
// build.debug_info on, which both reach the linker flags.
func TestI18nLanguagesWithOtherLinkerFlags(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "i18n")
	write(t, filepath.Join(dir, "tifa.yaml"), "i18n:\n  default: en-US\nsite:\n  url: https://i18n.example.com\nbuild:\n  debug_info: true\n")
	const version = "example.com/i18ntest/app/api/languages.Version"
	for _, flag := range []string{"-ldflags=-X=" + version + "=v1", "-ldflags=all=-X=" + version + "=v2"} {
		t.Run(flag, func(t *testing.T) { checkLinkerFlags(t, dir, flag, strings.HasSuffix(flag, "=v1")) })
	}
}

func checkLinkerFlags(t *testing.T, dir, flag string, v1 bool) {
	t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" "+flag))
	want := `"version":"v2"`
	if v1 {
		want = `"version":"v1"`
	}
	exe := buildAt(t, dir)
	base, _, _ := start(t, exe)
	// The default language comes first.
	_, _, body := cacheSend(t, "GET", base+"/api/languages", "")
	if !strings.Contains(body, `"atInit":["en-US","zh-CN"]`) || !strings.Contains(body, `"now":["en-US","zh-CN"]`) || !strings.Contains(body, want) {
		t.Errorf("%s", body)
	}
	// en-US is now the default language, so its prefix is a 308.
	if status, h, _ := cacheSend(t, "GET", base+"/en-US/store/mine", ""); status != 308 || h.Get("Location") != "/store/mine" {
		t.Errorf("/en-US/store/mine: %d Location %q", status, h.Get("Location"))
	}
}

// journalLinks are the hrefs in the body of the journal entry the API route at path answers.
func journalLinks(t *testing.T, base, path string) []string {
	t.Helper()
	status, _, body := cacheSend(t, "GET", base+path, "")
	var out struct{ Body string }
	if status != 200 || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("GET %s: %d %.300s", path, status, body)
	}
	var links []string
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(out.Body, -1) {
		links = append(links, m[1])
	}
	return links
}

// TestI18nContentLinks checks that the root-relative links in the Markdown of a journal entry get
// the prefix of the entry's language when the route has it, and are left alone otherwise.
func TestI18nContentLinks(t *testing.T) {
	skipUnlessEnabled(t)
	_, exe := build(t, "i18n")
	base, _, _ := start(t, exe)
	check := func(path string, want ...string) {
		t.Helper()
		if got := journalLinks(t, base, path); !slices.Equal(got, want) {
			t.Errorf("%s links:\n got %q\nwant %q", path, got, want)
		}
	}
	check("/store/journal",
		"/store/news", "/projects", "/favicon.svg", "/store?q=a#top", "/en-US/store/news", "https://example.org/store/news")
	check("/en-US/store/journal",
		"/en-US/store/news", "/projects", "/favicon.svg", "/en-US/store?q=a#top", "/zh-CN/store/news", "https://example.org/store/news")
}

// TestI18nPrefixAlways builds the app with i18n.prefix: always.
func TestI18nPrefixAlways(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "i18n")
	write(t, filepath.Join(dir, "tifa.yaml"), "i18n:\n  default: zh-CN\n  prefix: always\nsite:\n  url: https://i18n.example.com\n")
	exe := buildAt(t, dir)
	base, _, _ := start(t, exe)

	for _, tc := range []struct{ path, location string }{
		{"/store?q=a", "/zh-CN/store?q=a"},
		{"/store/mine?x=1", "/zh-CN/store/mine?x=1"},
		{"/store/news", "/zh-CN/store/news"},
	} {
		status, h, _ := cacheSend(t, "GET", base+tc.path, "")
		if status != 308 || h.Get("Location") != tc.location {
			t.Errorf("GET %s: %d Location %q, want 308 %q", tc.path, status, h.Get("Location"), tc.location)
		}
	}
	var m struct{ Language, URL string }
	_, _, body := cacheSend(t, "GET", base+"/zh-CN/store/mine?x=1", "")
	if err := json.Unmarshal([]byte(body), &m); err != nil || m.Language != "zh-CN" || m.URL != "/zh-CN/store/mine?x=1" {
		t.Errorf("/zh-CN/store/mine: %s", body)
	}
	_, _, body = cacheSend(t, "GET", base+"/en-US/store/mine", "")
	if err := json.Unmarshal([]byte(body), &m); err != nil || m.Language != "en-US" {
		t.Errorf("/en-US/store/mine: %s", body)
	}
	// The links of an entry get the prefix of its language, the default one too.
	if got := journalLinks(t, base, "/zh-CN/store/journal"); !slices.Equal(got, []string{
		"/zh-CN/store/news", "/projects", "/favicon.svg", "/zh-CN/store?q=a#top", "/en-US/store/news", "https://example.org/store/news",
	}) {
		t.Errorf("zh-CN journal links: %q", got)
	}
	// The versions of a page have the prefix of the default language, and x-default is that one.
	id := buildID(t, dir)
	_, _, answer := cacheSend(t, "GET", base+"/zh-CN/store/journal/ni-hao?_tifa="+id+".1", "")
	if want := `"alternates":{"list":[{"language":"zh-CN","url":"/zh-CN/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}]}`; !strings.Contains(answer, want) {
		t.Errorf("the versions of /zh-CN/store/journal/ni-hao: %s", answer)
	}
	// The pages carry the prefix on their links, in the default language too.
	for path, tc := range map[string][2]string{
		"/zh-CN/store": {"store go=zh-CN react=zh-CN path=/store", `href="/zh-CN/store/cart"`},
		"/en-US/store": {"store go=en-US react=en-US path=/store", `href="/en-US/store/cart"`},
	} {
		status, _, body := cacheSend(t, "GET", base+path, "")
		if status != 200 || !strings.Contains(plain(body), tc[0]) || !strings.Contains(body, tc[1]) || !strings.Contains(body, `href="/projects"`) || strings.Contains(body, "Page not found") {
			t.Errorf("GET %s: %d %.500s", path, status, plain(body))
		}
	}
	// Routes outside every area stay as they are.
	for path, want := range map[string]int{"/projects": 200, "/api/hook": 200, "/favicon.svg": 200, "/zh-CN/projects": 404, "/zh-CN/api/hook": 404} {
		if status, _, _ := cacheSend(t, "GET", base+path, ""); status != want {
			t.Errorf("GET %s: %d, want %d", path, status, want)
		}
	}
}

// TestI18nBuildFailures checks what tifa build says to the declarations of the languages that do
// not go together.
func TestI18nBuildFailures(t *testing.T) {
	skipUnlessEnabled(t)
	const ok = "i18n:\n  default: zh-CN\nsite:\n  url: https://i18n.example.com\n"
	for name, tc := range map[string]struct {
		yaml  string
		files map[string]string
		want  string
	}{
		"several languages and no default":              {"site:\n  url: https://i18n.example.com\n", nil, "i18n.default must name the default one"},
		"no site url":                                   {"i18n:\n  default: zh-CN\n", nil, "site.url must name the scheme and host"},
		"default no area has":                           {"i18n:\n  default: fr\nsite:\n  url: https://i18n.example.com\n", nil, `i18n.default is "fr"`},
		"area without the default":                      {ok, map[string]string{"app/admin/_locales/en-US.json": "{}", "app/admin/page.tsx": "export default function A() { return null }\n"}, "app/admin/_locales: the default language zh-CN has no file here"},
		"invalid tag":                                   {ok, map[string]string{"app/store/_locales/en_US.json": "{}"}, "app/store/_locales/en_US.json"},
		"lang in the htmlAttributes of the root Layout": {ok, map[string]string{"app/layout.tsx": "import { Outlet } from '@tanstack/react-router'\nexport const htmlAttributes = { lang: 'zh-CN' }\nexport default function Shell() { return <Outlet /> }\n"}, "app/layout.tsx:2:"},
		"lang in the htmlAttributes of a head":          {ok, map[string]string{"app/projects/page.tsx": "export const head = () => ({ htmlAttributes: { lang: 'en' } })\nexport default function P() { return null }\n"}, "app/projects/page.tsx:1:"},
		"top level folder like a language":              {ok, map[string]string{"app/en-US/page.tsx": "export default function A() { return null }\n"}, "app/en-US: the folder is named like the language en-US"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyApp(t, "i18n")
			write(t, filepath.Join(dir, "tifa.yaml"), tc.yaml)
			for f, body := range tc.files {
				write(t, filepath.Join(dir, f), body)
			}
			cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "app"))
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Errorf("tifa build: %v\n%s\nwant %q", err, out, tc.want)
			}
		})
	}
}

// TestDevLocaleFiles adds _locales/fr.json under tifa dev: /fr/store works without a restart, and
// removing the file makes it a 404 again.
func TestDevLocaleFiles(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	dir := copyApp(t, "i18n")
	d := startDev(t, dir)
	d.waitLog(t, 0, ready)
	v := d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" }).Version

	if status, _, _ := cacheSend(t, "GET", d.base+"/fr/store/mine", ""); status != 404 {
		t.Fatalf("GET /fr/store/mine before: %d", status)
	}
	mark := len(d.log.String())
	// The area needs the new language only for its own routes, and the site gets it.
	// The content is converted again too: the links of its Markdown follow the languages of the routes.
	write(t, filepath.Join(dir, "app/store/_locales/fr.json"), `{"title": "Magasin"}`)
	v = d.waitStatus(t, func(st devStatus) bool { return st.Version != v && st.Error == "" }).Version
	expectSteps(t, "added fr.json", d.waitLog(t, mark, ready), "static analysis", "code generation", "bundle", "content", "go build")
	var m struct{ Language, URL string }
	status, _, body := cacheSend(t, "GET", d.base+"/fr/store/mine", "")
	if err := json.Unmarshal([]byte(body), &m); status != 200 || err != nil || m.Language != "fr" || m.URL != "/fr/store/mine" {
		t.Fatalf("GET /fr/store/mine after: %d %s", status, body)
	}
	if _, _, body := cacheSend(t, "GET", d.base+"/api/languages", ""); !strings.Contains(body, `"now":["zh-CN","en-US","fr"]`) {
		t.Errorf("/api/languages: %s", body)
	}

	mark = len(d.log.String())
	if err := os.Remove(filepath.Join(dir, "app/store/_locales/fr.json")); err != nil {
		t.Fatal(err)
	}
	d.waitStatus(t, func(st devStatus) bool { return st.Version != v && st.Error == "" })
	d.waitLog(t, mark, ready)
	if status, _, _ := cacheSend(t, "GET", d.base+"/fr/store/mine", ""); status != 404 {
		t.Errorf("GET /fr/store/mine after removing fr.json: %d", status)
	}
}

// TestI18nInBrowser drives test/apps/i18n in a real browser: which links stay in the document,
// which load another one, the URLs of the data requests and of a handler call, a redirect and a
// preload across languages, and back and forward.
func TestI18nInBrowser(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	dir, exe := build(t, "i18n")
	base, _, _ := start(t, exe)

	pw, _ := openBrowser(t, "i18n-browser")
	eval := func(fn string) any {
		t.Helper()
		out := pw("eval", fn)
		_, result, ok := strings.Cut(out, "### Result\n")
		if !ok {
			t.Fatalf("no result in %s", out)
		}
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &v); err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		return v
	}
	hydrated := `() => { const root = document.getElementById("tifa-root"); return !!root && Object.keys(root).some((k) => k.startsWith("__reactContainer$")) }`
	here := `() => location.pathname + location.search`
	text := `() => document.querySelector("main")?.textContent ?? ""`
	flagged := `() => window.__sameDocument === true`
	click := func(id string) {
		t.Helper()
		pw("run-code", fmt.Sprintf(`async (page) => { await page.locator('#%s').click() }`, id))
	}
	// open loads a page and waits for it to hydrate. check fails on a warning in the console,
	// which is where React reports a hydration mismatch, and on a failed request.
	failed := regexp.MustCompile(`=> \[(?:FAILED|[45]\d\d)\]`)
	check := func(where string) {
		t.Helper()
		if out := pw("console", "warning"); !strings.Contains(out, "Total messages: 0 ") {
			t.Errorf("%s: console:\n%s", where, out)
		}
		if out := pw("network"); failed.MatchString(out) {
			t.Errorf("%s: failed requests:\n%s", where, out)
		}
	}
	first := true
	open := func(path, want string) {
		t.Helper()
		if first {
			pw("open", base+path)
			first = false
		} else {
			pw("goto", base+path)
		}
		ready := func() bool { return eval(hydrated) == true && strings.Contains(eval(text).(string), want) }
		for deadline := time.Now().Add(10 * time.Second); !ready() && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		}
		if !ready() {
			t.Fatalf("%s did not hydrate with %q: at %v, hydrated %v, text %q\nconsole:\n%s\nnetwork:\n%s", path, want, eval(here), eval(hydrated), eval(text), pw("console"), pw("network"))
		}
		check(path)
		eval(`() => { window.__sameDocument = true; return true }`)
	}
	shows := func(path, want string, same bool) {
		t.Helper()
		waitFor(t, path+" to show", func() bool { return eval(here) == path && strings.Contains(eval(text).(string), want) })
		if got := eval(flagged) == true; got != same {
			t.Errorf("%s: the same document is %v, want %v", path, got, same)
		}
	}
	network := func() string { return pw("network") }

	// First load of each language, with real SSR and without.
	open("/en-US/store", "store go=en-US react=en-US")
	open("/store", "store go=zh-CN react=zh-CN")
	open("/en-US/store/csr", "csr go=en-US react=en-US")
	open("/store/csr", "csr go=zh-CN react=zh-CN")

	// A query spelled another way than the router writes it (%20 for +, %2F for /), an empty value
	// and a key given twice around another stay in the address bar as they were typed, with a
	// prefix and without, and the page asks for no data after it hydrates.
	for _, c := range []struct{ path, want string }{
		{"/en-US/store?q=a%20b&utm=x%2Fy", "store go=en-US react=en-US path=/store q=a b"},
		{"/en-US/store?utm=&q=a%2Fb&utm=c%20d", "store go=en-US react=en-US path=/store q=a/b"},
		{"/store?q=a%20b&e=&q=c%2Fd", "store go=zh-CN react=zh-CN path=/store q=a b"},
		{"/en-US/store/csr?x=a%20b&y=&x=%2F", "csr go=en-US react=en-US"},
		{"/store/csr?x=a%20b&y=&x=%2F", "csr go=zh-CN react=zh-CN"},
		{"/projects?next=%2Fa&q=a%20b", "projects go=zh-CN react=zh-CN"},
	} {
		open(c.path, c.want)
		// The router replaces the address bar and loads the page again right after hydration.
		time.Sleep(500 * time.Millisecond)
		if got := eval(here); got != c.path {
			t.Errorf("%s: the address bar shows %v", c.path, got)
		}
		if got := eval(`() => performance.getEntriesByType("resource").map((e) => e.name).filter((n) => n.includes("_tifa=")).join(" ")`); got != "" {
			t.Errorf("%s: asked for data after hydrating: %v", c.path, got)
		}
		check(c.path)
	}

	// In en-US: a link to a page of the area stays in the document and gets the prefix, with the
	// data request of that URL. Back and forward follow.
	open("/en-US/store", "store go=en-US")
	click("to-cart")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", true)
	if out := network(); !strings.Contains(out, "/en-US/store/cart?_tifa=") {
		t.Errorf("the data request of the cart has no prefix:\n%s", out)
	}
	pw("go-back")
	shows("/en-US/store", "store go=en-US", true)
	pw("go-forward")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", true)
	click("back")
	shows("/en-US/store", "store go=en-US", true)

	// A handler of the area is called at the URL of the page's language.
	click("call")
	waitFor(t, "the handler to answer", func() bool { return eval(`() => document.getElementById("mine")?.textContent`) == "en-US" })
	if out := network(); !strings.Contains(out, "/en-US/store/mine") {
		t.Errorf("the handler call has no prefix:\n%s", out)
	}

	// A preload of a page of the document's language is asked for with the prefix, one of another
	// language asks for nothing.
	pw("run-code", `async (page) => { await page.locator('#to-cart').hover(); await page.waitForTimeout(400) }`)
	if out := network(); !strings.Contains(out, "/en-US/store/cart?_tifa=") {
		t.Errorf("no preload of the cart:\n%s", out)
	}
	pw("run-code", `async (page) => { await page.locator('#to-projects').hover(); await page.waitForTimeout(400) }`)
	if out := network(); strings.Contains(out, "/projects?_tifa=") {
		t.Errorf("a preload of a page outside the language:\n%s", out)
	}

	// The versions a Load set reach the head and the language switch after a navigation in en-US.
	click("to-entry")
	shows("/en-US/store/journal/hello", "entry hello", true)
	versions := `() => [...document.querySelectorAll('head link[hreflang], main nav a')].map((e) => (e.getAttribute('hreflang') ?? e.textContent) + ' ' + e.getAttribute('href')).join('|')`
	waitFor(t, "the versions of the entry", func() bool { return strings.Contains(eval(versions).(string), "/store/journal/ni-hao") })
	if got := eval(versions).(string); got != "zh-CN https://i18n.example.com/store/journal/ni-hao|en-US https://i18n.example.com/en-US/store/journal/hello|x-default https://i18n.example.com/store/journal/ni-hao|zh-CN /store/journal/ni-hao|en-US /en-US/store/journal/hello" {
		t.Errorf("the versions of /en-US/store/journal/hello: %s", got)
	}
	pw("go-back")
	shows("/en-US/store", "store go=en-US", true)

	// A link to a page outside every area loads a document, without a prefix.
	click("to-projects")
	shows("/projects", "projects go=zh-CN react=zh-CN", false)
	// And from there a link into the default language stays in the document.
	eval(`() => { window.__sameDocument = true; return true }`)
	click("to-store")
	shows("/store", "store go=zh-CN react=zh-CN", true)

	// A redirect of a Load to a page outside the language loads that document. One to a page of the
	// language stays.
	open("/en-US/store", "store go=en-US")
	click("to-stay")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", true)
	open("/en-US/store", "store go=en-US")
	click("to-leave")
	shows("/projects", "projects go=zh-CN react=zh-CN", false)

	// navigate({ href }) with the prefix of another language loads that document, with the prefix of
	// this one stays. The default language has no prefix: /store/cart names zh-CN.
	open("/en-US/store", "store go=en-US")
	click("nav-default")
	shows("/store/cart", "cart go=zh-CN react=zh-CN", false)
	open("/en-US/store", "store go=en-US")
	click("nav-english")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", true)
	open("/store", "store go=zh-CN")
	click("nav-english")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", false)
	// So does navigate({ to }) with the prefix of another language, and a URL of another language
	// pushed on the history.
	open("/store", "store go=zh-CN")
	click("to-english")
	shows("/en-US/store/cart", "cart go=en-US react=en-US", false)
	open("/en-US/store", "store go=en-US")
	click("push-default")
	shows("/store/cart", "cart go=zh-CN react=zh-CN", false)

	// A trailing slash is redirected away, with the prefix kept.
	pw("goto", base+"/en-US/store/")
	waitFor(t, "the trailing slash to go", func() bool { return eval(here) == "/en-US/store" })

	// The same links in the default language.
	open("/store", "store go=zh-CN")
	click("to-cart")
	shows("/store/cart", "cart go=zh-CN react=zh-CN", true)
	_ = dir
}
