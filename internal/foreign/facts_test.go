package foreign

import (
	"strings"
	"testing"
)

func TestCheckFactsReportsImportsOfOtherFrameworks(t *testing.T) {
	f := &Facts{Imports: []Import{
		{"next/link", 1, 1}, {"next", 2, 1}, {"next/navigation", 3, 1}, {"react-router-dom", 4, 1}, {"react-router/dom", 5, 1},
		{"@remix-run/react", 6, 1}, {"@tanstack/react-start", 7, 1}, {"@tanstack/react-start/server", 8, 1}, {"@tanstack/start-client-core", 9, 1},
		{"$app/navigation", 10, 1}, {"@sveltejs/kit", 11, 1}, {"astro:content", 12, 1}, {"astro:transitions", 13, 1},
		// Legal: TanStack Router and Query, and packages whose names only start like another framework.
		{"@tanstack/react-router", 20, 1}, {"@tanstack/react-query", 21, 1}, {"@tanstack/router-core", 22, 1},
		{"next-themes", 23, 1}, {"nextra", 24, 1}, {"react-router-config", 25, 1}, {"tifa/client", 26, 1}, {"./next/link", 27, 1},
	}}
	got := CheckFacts("app/util.ts", f)
	if len(got) != 13 {
		t.Fatalf("got %d findings, want 13:\n%s", len(got), strings.Join(lines(got), "\n"))
	}
	for i, f := range got {
		if f.Warning || f.Line != i+1 || !strings.Contains(f.Msg, ". In Tifa: ") || !strings.Contains(f.Msg, ". See docs/") {
			t.Errorf("finding %d: %q", i, f.String())
		}
	}
	if !strings.HasPrefix(got[0].Msg, "next/link is a module of Next.js") || !strings.Contains(got[0].Msg, "@tanstack/react-router") {
		t.Errorf("next/link: %q", got[0].Msg)
	}
	if !strings.HasPrefix(got[7].Msg, "@tanstack/react-start/server is a module of TanStack Start") || !strings.Contains(got[7].Msg, "route.gen") {
		t.Errorf("react-start: %q", got[7].Msg)
	}
}

func TestCheckFactsTellsUseClientFromUseServer(t *testing.T) {
	got := CheckFacts("app/x.tsx", &Facts{Directives: []Directive{{"use strict", 1, 1}, {"use client", 2, 1}, {"use server", 3, 1}}})
	if len(got) != 2 {
		t.Fatalf("got %v", lines(got))
	}
	if !got[0].Warning || got[0].Line != 2 || !strings.HasPrefix(got[0].Msg, "'use client' is a directive of React Server Components") {
		t.Errorf("use client: %q", got[0].String())
	}
	if got[1].Warning || got[1].Line != 3 || !strings.Contains(got[1].Msg, "method handler") {
		t.Errorf("use server: %q", got[1].String())
	}
}

func TestCheckFactsReadsTheExportsOfPagesAndLayoutsOnly(t *testing.T) {
	exports := []Export{
		{"getServerSideProps", "function", "", 1, 1}, {"getStaticProps", "function", "", 2, 1}, {"getStaticPaths", "other", "", 3, 1},
		{"generateMetadata", "function", "", 4, 1}, {"generateStaticParams", "function", "", 5, 1}, {"metadata", "object", "", 6, 1},
		{"loader", "function", "", 7, 1}, {"action", "function", "", 8, 1}, {"clientLoader", "function", "", 9, 1}, {"clientAction", "function", "", 10, 1},
		{"revalidate", "number", "60", 11, 1}, {"revalidate", "boolean", "false", 12, 1}, {"dynamic", "string", "force-dynamic", 13, 1},
		// Names a page may use for its own: another kind of value than the framework's.
		{"action", "string", "save", 20, 1}, {"loader", "object", "", 21, 1}, {"metadata", "string", "x", 22, 1}, {"revalidate", "function", "", 23, 1},
		{"dynamic", "boolean", "true", 24, 1}, {"dynamic", "string", "left", 25, 1}, {"head", "function", "", 26, 1}, {"Error", "function", "", 27, 1}, {"htmlAttributes", "object", "", 28, 1},
	}
	for _, file := range []string{"app/page.tsx", "app/store/layout.tsx", "app/store/layout_.tsx"} {
		got := CheckFacts(file, &Facts{Exports: exports})
		if len(got) != 13 {
			t.Fatalf("%s: got %d findings, want 13:\n%s", file, len(got), strings.Join(lines(got), "\n"))
		}
		for i, f := range got {
			if !f.Warning || f.Line != i+1 {
				t.Errorf("%s finding %d: %q", file, i, f.String())
			}
		}
	}
	if got := CheckFacts("app/helpers.ts", &Facts{Exports: exports}); len(got) != 0 {
		t.Errorf("a file that is no page or layout has findings: %v", lines(got))
	}
	got := CheckFacts("app/page.tsx", &Facts{Exports: exports[:1]})
	if !strings.HasPrefix(got[0].Msg, "getServerSideProps is the data function of Next.js pages") || !strings.Contains(got[0].Msg, "func Load(c *tifa.Ctx)") {
		t.Errorf("getServerSideProps: %q", got[0].Msg)
	}
}

func TestCheckFactsReportsCallsOfImportedRouteFunctionsInPages(t *testing.T) {
	calls := []Call{
		{"createFileRoute", "@tanstack/react-router", 1, 1}, {"createRootRoute", "@tanstack/react-router", 2, 1},
		{"createServerFn", "./server-fn", 3, 1},
		// Already reported by its import.
		{"createServerFn", "@tanstack/react-start", 4, 1},
		// Not imported: the app's own function, or a global.
		{"createFileRoute", "", 5, 1},
	}
	got := CheckFacts("app/page.tsx", &Facts{Calls: calls})
	if len(got) != 3 {
		t.Fatalf("got %v", lines(got))
	}
	for _, f := range got {
		if !f.Warning {
			t.Errorf("%q is not a warning", f.String())
		}
	}
	if got := CheckFacts("app/util.ts", &Facts{Calls: calls}); len(got) != 0 {
		t.Errorf("a file that is no page or layout has findings: %v", lines(got))
	}
}

func TestNextImageAndFontPointToTheDocOnImagesAndFonts(t *testing.T) {
	f := &Facts{Imports: []Import{{"next/image", 1, 1}, {"next/font/google", 2, 1}, {"next/font/local", 3, 1}, {"next/font", 4, 1}}}
	got := CheckFacts("app/util.ts", f)
	if len(got) != 4 {
		t.Fatalf("got %d findings, want 4:\n%s", len(got), strings.Join(lines(got), "\n"))
	}
	for _, f := range got {
		if !strings.HasSuffix(f.Msg, "See docs/app.md#images-and-fonts") {
			t.Errorf("%q", f.String())
		}
	}
}
