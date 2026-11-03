package scan

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
)

var update = flag.Bool("update", false, "rewrite the golden files")

type post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}

type doc struct {
	content.Entry
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
}

type note struct {
	content.Entry
	Title string `json:"title"`
}

type image struct {
	Src   string `json:"src"`
	Width int    `json:"width,omitempty"`
}

type richPost struct {
	content.Entry
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Updated     *time.Time     `json:"updated"`
	Tags        []string       `json:"tags"`
	Cover       string         `json:"cover,omitempty"`
	Count       int8           `json:"count,omitempty"`
	Ratio       float64        `json:"ratio,omitempty"`
	Meta        map[string]int `json:"meta"`
	Image       *image         `json:"image"`
}

func collection(t *testing.T, name string, schema any, langs []string, match string) Collection {
	t.Helper()
	typ := reflect.TypeOf(schema)
	tree, problems := FromReflect(typ)
	if len(problems) > 0 {
		t.Fatalf("schema %s: %v", typ, problems)
	}
	return Collection{Name: name, Languages: langs, Match: match, Schema: tree, Type: typ.String(), LanguagesAt: "content.go:9:60"}
}

// golden compares what a scan found with testdata/<name>.golden.
func golden(t *testing.T, name string, res Result) {
	t.Helper()
	var b strings.Builder
	for _, e := range res.Errors {
		b.WriteString(e.Error() + "\n")
	}
	for _, w := range res.Warnings {
		b.WriteString("warning: " + w + "\n")
	}
	if len(res.File) > 0 {
		data, err := res.File.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		v := jsontext.Value(data)
		if err := v.Indent(jsontext.WithIndent("  ")); err != nil {
			t.Fatal(err)
		}
		b.Write(v)
		b.WriteString("\n")
	}
	file := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("scan of %s differs from %s (go test -update to rewrite it):\n%s", name, file, b.String())
	}
}

func TestScanBlogAndDocs(t *testing.T) {
	langs := []string{"zh-CN", "en"}
	res := Scan(filepath.Join("testdata", "blog"), []Collection{
		collection(t, "blog", post{}, langs, ""),
		collection(t, "docs", doc{}, langs, ""),
	}, []string{"blog", "docs"}, Options{})
	golden(t, "blog", res)
	if data, _ := res.File.Marshal(); strings.Contains(string(data), "DRAFT") || strings.Contains(string(data), "wip") {
		t.Errorf("a draft is in content.json:\n%s", data)
	}
}

// Each entry that goes into the build is handed to the Vite plugin with its front matter left as
// empty lines, where its body goes in dist, and the folder of its version in the default language.
func TestScanListsTheMarkdownToConvert(t *testing.T) {
	res := Scan(filepath.Join("testdata", "blog"), []Collection{
		collection(t, "blog", post{}, []string{"zh-CN", "en"}, ""),
	}, []string{"blog", "docs"}, Options{})
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	byOut := map[string]internalcontent.Markdown{}
	for _, m := range res.Markdown {
		byOut[m.Out] = m
	}
	if len(byOut) != len(res.File["blog"]) {
		t.Errorf("%d entries to convert for %d records", len(byOut), len(res.File["blog"]))
	}
	for _, r := range res.File["blog"] {
		if _, ok := byOut[r.File]; !ok || r.File != internalcontent.BodyFile("blog", r.Lang, r.ID) {
			t.Errorf("record %s %s has the file %q, which is not converted", r.Lang, r.ID, r.File)
		}
	}
	en := byOut["content/blog/en/202608/free-search.html"]
	want := internalcontent.Markdown{
		Collection: "blog", Lang: "en", ID: "202608/free-search",
		Source:   "content/blog/en/202608/free-search/index.md",
		Fallback: "content/blog/zh-CN/202608/free-search",
		Out:      "content/blog/en/202608/free-search.html",
		Text:     strings.Repeat("\n", 8) + "\n![Architecture](./diagram.png)\n\nThe model reasons well but doesn't know what happened today. See <https://example.org/search>.\n\n    indented code does not count\n",
	}
	got := en
	got.Frontmatter = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entry to convert\n%+v\nwant\n%+v", got, want)
	}
	if !strings.Contains(string(en.Frontmatter), `"title":"Free web search for your model"`) {
		t.Errorf("front matter %s", en.Frontmatter)
	}
	// Translations linked by translationKey fall back to each other's folder.
	if m := byOut["content/blog/en/202611/release-notes.html"]; m.Fallback != "content/blog/zh-CN/202611/launch" {
		t.Errorf("release-notes falls back to %q", m.Fallback)
	}
	if m := byOut["content/blog/zh-CN/202608/free-search.html"]; m.Fallback != "" {
		t.Errorf("an entry of the default language falls back to %q", m.Fallback)
	}
	for _, m := range res.Markdown {
		if strings.Contains(m.Text, "DRAFT") {
			t.Errorf("the draft %s is converted", m.Source)
		}
	}
}

func TestScanKeepsDraftsForDev(t *testing.T) {
	res := Scan(filepath.Join("testdata", "blog"), []Collection{
		collection(t, "blog", post{}, []string{"zh-CN", "en"}, ""),
	}, []string{"blog", "docs"}, Options{Drafts: true})
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	for _, r := range res.File["blog"] {
		if r.ID == "202610/wip" {
			if !r.Draft {
				t.Errorf("the draft has Draft false: %+v", r)
			}
			return
		}
	}
	t.Errorf("no draft in %+v", res.File["blog"])
}

func TestScanWithoutLanguages(t *testing.T) {
	res := Scan(filepath.Join("testdata", "notes"), []Collection{collection(t, "notes", note{}, nil, "")}, []string{"notes"}, Options{})
	golden(t, "notes", res)
}

func TestScanWithMatch(t *testing.T) {
	res := Scan(filepath.Join("testdata", "notes"), []Collection{collection(t, "notes", note{}, nil, "*/*/index.md")}, []string{"notes"}, Options{})
	golden(t, "notes-match", res)
}

func TestScanFileErrors(t *testing.T) {
	langs := []string{"zh-CN", "en"}
	res := Scan(filepath.Join("testdata", "badfiles"), []Collection{
		collection(t, "blog", post{}, langs, ""),
		collection(t, "docs", doc{}, []string{"en"}, ""),
		collection(t, "missing", doc{}, nil, ""),
	}, []string{"blog", "docs", "missing"}, Options{})
	golden(t, "badfiles", res)
}

func TestScanFrontMatterErrors(t *testing.T) {
	res := Scan(filepath.Join("testdata", "badmatter"), []Collection{
		collection(t, "blog", richPost{}, []string{"zh-CN", "en"}, ""),
	}, []string{"blog"}, Options{})
	golden(t, "badmatter", res)
}

func TestFrontMatterBecomesTheJSONOfTheSchema(t *testing.T) {
	tree, _ := FromReflect(reflect.TypeFor[richPost]())
	src, err := os.ReadFile(filepath.Join("testdata", "badmatter", "content", "blog", "zh-CN", "ok", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := parseMatter(src, tree, "scan.richPost")
	if len(m.errs) > 0 {
		t.Fatal(m.errs)
	}
	want := `{"title":"ok","description":"every kind the schema holds","updated":"2026-08-09T10:00:00+08:00","tags":["a","b"],"count":-128,"ratio":0.5,"meta":{"stars":3},"image":{"src":"/cover.jpg"}}`
	if string(m.data) != want {
		t.Errorf("data = %s\nwant   %s", m.data, want)
	}
	var p richPost
	if err := json.Unmarshal(m.data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Updated == nil || !p.Updated.Equal(time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)) || p.Count != -128 || p.Image.Src != "/cover.jpg" || p.Meta["stars"] != 3 {
		t.Errorf("decoded %+v", p)
	}
}

func TestDates(t *testing.T) {
	tree, _ := FromReflect(reflect.TypeFor[note]())
	for _, c := range []struct {
		src  string
		want time.Time
	}{
		{"date: 2026-11-20", time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)},
		{`date: "2026-11-20"`, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)},
		{"date: 2026-11-20T09:00:00+08:00", time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)},
		{"date: 2026-11-20T09:00:00Z", time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)},
	} {
		m := parseMatter([]byte("---\ntitle: x\n"+c.src+"\n---\n"), tree, "note")
		if len(m.errs) > 0 || !m.date.Equal(c.want) {
			t.Errorf("%s: date %v, errors %v, want %v", c.src, m.date, m.errs, c.want)
		}
	}
}
