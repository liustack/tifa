package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
)

type serverPost struct {
	content.Entry
	Title string `json:"title"`
}

func (p serverPost) Validate() error {
	if p.Title == "" {
		return errors.New("title is empty")
	}
	return nil
}

// definePosts declares a collection for the test t only: the other tests of the server run apps
// without content.json, which a declared collection needs.
func definePosts(t *testing.T) *content.Collection[serverPost] {
	c := content.Define[serverPost](content.Source{Dir: "serverposts"})
	t.Cleanup(func() { internalcontent.Unregister(c) })
	return c
}

// distWithContent is the dist/ of a build without pages whose content.json holds one entry with
// the title title.
func distWithContent(title string) fstest.MapFS {
	return fstest.MapFS{
		"dist/build.json":   {Data: []byte(`{"buildId":"t","head":""}`)},
		"dist/content.json": {Data: []byte(`{"serverposts":[{"id":"a","slug":"a","path":"a","group":"a","words":2,"source":"content/serverposts/a.md","data":{"title":"` + title + `"}}]}`)},
	}
}

var errStop = errors.New("stop here")

func TestContentIsLoadedBeforeOnStart(t *testing.T) {
	serverPosts := definePosts(t)
	var titles []string
	app := App{Dist: distWithContent("A"), Hooks: Hooks{OnStart: func(context.Context) error {
		for _, p := range serverPosts.All() {
			titles = append(titles, p.Title)
		}
		return errStop
	}}}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0")
	if !errors.Is(err, errStop) {
		t.Fatalf("serve: %v", err)
	}
	if !slices.Equal(titles, []string{"A"}) {
		t.Errorf("OnStart saw %q", titles)
	}
}

func TestContentThatFailsValidateStopsTheStart(t *testing.T) {
	definePosts(t)
	started := false
	app := App{Dist: distWithContent(""), Hooks: Hooks{OnStart: func(context.Context) error {
		started = true
		return errStop
	}}}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0")
	if err == nil || err.Error() != "tifa: content: content/serverposts/a.md: Validate: title is empty" {
		t.Fatalf("serve: %v", err)
	}
	if started {
		t.Error("OnStart ran")
	}
}

func TestContentNeedsTheContentFileOfTheBuild(t *testing.T) {
	definePosts(t)
	dist := fstest.MapFS{"dist/build.json": {Data: []byte(`{"buildId":"t","head":""}`)}}
	_, _, err := runApp(t, context.Background(), App{Dist: dist}, nil, "--server-addr", "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "content.json") {
		t.Fatalf("serve: %v", err)
	}
}
