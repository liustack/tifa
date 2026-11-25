package tifatest

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
	"github.com/liustack/tifa/server"
)

// An app under Start has no front end, and the files of its site answer all the same, as a test of
// the sitemap in the guide reads them.
func TestStartAnswersTheFilesOfTheSite(t *testing.T) {
	SetContent(t, testBlog, testPost{Entry: content.Entry{ID: "a", Language: "en"}, Title: "A"})
	note := testBlog.All()[0]
	app := server.App{
		SiteURL: "https://notes.example.com",
		Hooks: server.Hooks{OnStart: func(context.Context) error {
			tifa.SetSitemap(func(c *tifa.Ctx) (tifa.Sitemap, error) {
				return tifa.Sitemap{Name: "Notes", Pages: []tifa.SitemapPage{{Path: "/notes/a", Title: "A", Content: note}}}, nil
			})
			return nil
		}},
		Routes: []server.Route{{ID: "app/notes/note+/route", Path: "/notes/{note}", Page: true, SSR: true, Layouts: []string{}}},
	}
	srv := Start(t, app)
	for _, tc := range []struct{ path, want string }{
		{"/sitemap.xml", "<loc>https://notes.example.com/notes/a</loc>"},
		{"/llms.txt", "[A](https://notes.example.com/notes/a.md)"},
		{"/notes/a.md", "# A\n"},
	} {
		res, err := srv.Client().Get(srv.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), tc.want) {
			t.Errorf("GET %s: %d %s, want %s", tc.path, res.StatusCode, body, tc.want)
		}
	}
}
