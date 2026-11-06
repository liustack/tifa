package codegen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
)

// i18nManifest is a store in two languages next to a workbench without any.
func i18nManifest() *analyze.Manifest {
	both := []string{"zh-CN", "en-US"}
	return &analyze.Manifest{
		Module: "example.com/x",
		I18n: &analyze.I18n{Default: "zh-CN", Languages: both, SiteURL: "https://a.example.com",
			Areas: []analyze.Area{{Dir: "app/store", Languages: both}}},
		Layouts: []analyze.Layout{
			{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: []string{"app/projects/route", "app/store/route"}},
			{ID: "app/store/layout", Dir: "app/store", Path: "/store", TSX: true, SSR: true, Parent: "app/layout", Area: "app/store", Languages: both, Routes: []string{"app/store/route"}},
		},
		Routes: []analyze.Route{
			{ID: "app/projects/route", Dir: "app/projects", Path: "/projects", Page: true, SSR: true, RenderCache: true, Layouts: []string{"app/layout"}},
			{ID: "app/store/route", Dir: "app/store", Path: "/store", Page: true, SSR: true, RenderCache: true, Layouts: []string{"app/layout", "app/store/layout"}, Area: "app/store", Languages: both},
		},
	}
}

func writeI18nApp(t *testing.T) string {
	root := t.TempDir()
	const component = "export default function X() { return null }\n"
	for _, f := range []string{"app/layout.tsx", "app/projects/page.tsx", "app/store/layout.tsx", "app/store/page.tsx"} {
		writeFile(t, filepath.Join(root, f), component)
	}
	return root
}

func TestAppGoCarriesTheLanguages(t *testing.T) {
	m := i18nManifest()
	files, err := Generate(writeI18nApp(t), m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	app := strings.Join(strings.Fields(string(files["tifaapp/app.go"])), " ")
	for _, want := range []string{
		"I18n: &server.I18n{",
		`Default: "zh-CN"`,
		`Languages: []string{"zh-CN", "en-US"}`,
		`SiteURL: "https://a.example.com"`,
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app.go lacks %s:\n%s", want, app)
		}
	}
	if strings.Count(app, `Languages: []string{"zh-CN", "en-US"},`) != 3 { // the site, the store Layout, the store route
		t.Errorf("app.go:\n%s", app)
	}
	// An app without languages writes none of it.
	m.I18n = nil
	for i := range m.Layouts {
		m.Layouts[i].Area, m.Layouts[i].Languages = "", nil
	}
	for i := range m.Routes {
		m.Routes[i].Area, m.Routes[i].Languages = "", nil
	}
	files, err = Generate(writeI18nApp(t), m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	if app := string(files["tifaapp/app.go"]); strings.Contains(app, "Languages") || strings.Contains(app, "I18n") {
		t.Errorf("app.go of an app without languages:\n%s", app)
	}
}

func TestLanguageTableOfTheBrowser(t *testing.T) {
	files, err := Generate(writeI18nApp(t), i18nManifest(), false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	want := tsHeader + `
// The languages of the app, from its _locales folders and tifa.yaml. routes lists the languages
// of the routes of the route tree that are in an area, by their TanStack route IDs: a Layout, a
// page, and the route that shows the 404 of a Layout. A route outside every area has none, is
// never prefixed, and is not here.
export const i18n: I18n | undefined = {
  default: "zh-CN",
  always: false,
  languages: ["zh-CN", "en-US"],
  routes: {
    "/_app/store": ["zh-CN", "en-US"],
    "/_app/store/": ["zh-CN", "en-US"],
  },
}

export interface I18n {
  readonly default: string
  readonly always: boolean
  readonly languages: readonly string[]
  readonly routes: { readonly [routeId: string]: readonly string[] }
}
`
	if got := string(files["i18n.gen.ts"]); got != want {
		t.Errorf("i18n.gen.ts =\n%s\nwant\n%s", got, want)
	}
	m := i18nManifest()
	m.I18n = nil
	files, err = Generate(writeI18nApp(t), m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(files["i18n.gen.ts"]); !strings.Contains(got, "export const i18n: I18n | undefined = undefined") {
		t.Errorf("an app without languages:\n%s", got)
	}
}

func TestHandlerFunctionsOfAnAreaCallThePublicPath(t *testing.T) {
	m := i18nManifest()
	both := []string{"zh-CN", "en-US"}
	m.Routes = append(m.Routes,
		analyze.Route{ID: "app/store/mine/route", Dir: "app/store/mine", Path: "/store/mine", Handlers: []analyze.Handler{{Method: "POST"}}, Area: "app/store", Languages: both},
		analyze.Route{ID: "app/api/hook/route", Dir: "app/api/hook", Path: "/api/hook", Handlers: []analyze.Handler{{Method: "POST"}}},
	)
	files, err := Generate(writeI18nApp(t), m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	mine := string(files["app/store/mine/route.gen.ts"])
	for _, want := range []string{`import { call, publicPath, type CallOptions } from "../../../runtime/call"`, "call(\"POST\", publicPath(`/store/mine`, [\"zh-CN\", \"en-US\"]), options"} {
		if !strings.Contains(mine, want) {
			t.Errorf("the handler of an area lacks %s:\n%s", want, mine)
		}
	}
	// A route outside every area is called as before.
	hook := string(files["app/api/hook/route.gen.ts"])
	if strings.Contains(hook, "publicPath") || !strings.Contains(hook, "call(\"POST\", `/api/hook`, options") {
		t.Errorf("the handler outside every area:\n%s", hook)
	}
}
