package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var reactText = regexp.MustCompile(`<!-- -->`)

// plain is the body without the markers React writes between texts.
func plain(body string) string { return reactText.ReplaceAllString(body, "") }

// TestI18nApp builds test/apps/i18n, a store in zh-CN and en-US next to a page and an API route
// outside every area, and checks from outside what the server does with the language of a URL:
// the 308s and 404s, the language and the URL the code of the app sees, the whole-page cache and
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
		if status, _, body := cacheSend(t, "GET", base+"/store?q=a", ""); status != 200 || !strings.Contains(plain(body), "store go=zh-CN react=zh-CN path=/store q=a") {
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

	t.Run("the whole-page cache, the render cache and the ETag never cross languages", func(t *testing.T) {
		news := func(path string) (string, string, string) {
			t.Helper()
			status, h, body := cacheSend(t, "GET", base+path, "")
			if status != 200 {
				t.Fatalf("GET %s: %d %.300s", path, status, body)
			}
			return h.Get("ETag"), h.Get("Cache-Status"), body
		}
		zhTag, zhStatus, zhBody := news("/store/news")
		enTag, enStatus, _ := news("/en-US/store/news")
		if zhStatus != "tifa; fwd=miss" || enStatus != "tifa; fwd=miss" {
			t.Errorf("first requests: %q and %q, each language makes its own page", zhStatus, enStatus)
		}
		if zhTag == "" || zhTag == enTag {
			t.Errorf("ETags %q and %q, want two", zhTag, enTag)
		}
		if _, status, body := news("/store/news"); status != "tifa; hit" || body != zhBody {
			t.Errorf("second zh-CN request: %q", status)
		}
		if _, status, _ := news("/en-US/store/news"); status != "tifa; hit" {
			t.Errorf("second en-US request: %q", status)
		}
		// The tag of one language does not answer 304 for the other.
		if status, _, _ := cacheSend(t, "GET", base+"/en-US/store/news", "", "If-None-Match", zhTag); status == 304 {
			t.Errorf("the ETag of zh-CN answered 304 for en-US")
		}
		if status, _, _ := cacheSend(t, "GET", base+"/store/news", "", "If-None-Match", zhTag); status != 304 {
			t.Errorf("the ETag of zh-CN answered %d for zh-CN", status)
		}
		// The data is the same in both languages, and each language still made its own page: two
		// Loads ran, and neither render was answered by the render cache.
		_, _, zhData := cacheSend(t, "GET", data("/store/news", "1"), "")
		_, _, enData := cacheSend(t, "GET", data("/en-US/store/news", "1"), "")
		const same = `"data":{"body":"the same in every language"}`
		if !strings.Contains(zhData, same) || !strings.Contains(enData, same) {
			t.Errorf("data of zh-CN %s and of en-US %s", zhData, enData)
		}
		var loads struct{ News int64 }
		_, _, body := cacheSend(t, "GET", base+"/api/loads", "")
		if err := json.Unmarshal([]byte(body), &loads); err != nil || loads.News != 2 {
			t.Errorf("Loads of the page: %s, want 2, one for each language", body)
		}
		for _, path := range []string{"/store/news", "/en-US/store/news"} {
			if _, h, _ := cacheSend(t, "GET", base+path, ""); h.Get("Cache-Status") != "tifa; hit" {
				t.Errorf("GET %s: %v", path, h)
			}
		}
		// The page of the cache carries the language of its own entry.
		// (The list of versions names en-US too, so the language of the page is the one in front of it.)
		if !regexp.MustCompile(`dehydratedData:\$R\[\d+\]=\{buildId:"[^"]*",language:"zh-CN"`).MatchString(zhBody) {
			t.Errorf("the hydration data of the zh-CN page: %.200s", zhBody)
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
		// An entry in one language has a list of one. The other pages say nothing: the browser
		// works out their versions from the table of the build.
		_, _, body = cacheSend(t, "GET", data("/store/journal/only-zh", "1"), "")
		if !strings.Contains(body, `"alternates":{"list":[{"language":"zh-CN","url":"/store/journal/only-zh"}]}`) {
			t.Errorf("%s", body)
		}
		for _, path := range []string{"/store?q=a", "/en-US/store/news", "/projects"} {
			if _, _, body := cacheSend(t, "GET", data(path, "1"), ""); strings.Contains(body, "alternates") {
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
	write(t, filepath.Join(dir, "app/store/_locales/fr.json"), `{"title": "Magasin"}`)
	v = d.waitStatus(t, func(st devStatus) bool { return st.Version != v && st.Error == "" }).Version
	expectSteps(t, "added fr.json", d.waitLog(t, mark, ready), "static analysis", "code generation", "bundle", "go build")
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
