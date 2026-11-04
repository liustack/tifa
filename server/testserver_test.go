package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/liustack/tifa"
)

// testBundle is a stand-in for the SSR bundle of tifa build that renders the texts of the Loads
// after mark.
func testBundle(mark string) fstest.MapFS {
	return fstest.MapFS{
		"dist/build.json":   {Data: []byte(`{"buildId":"b-` + mark + `","head":"<script type=\"module\" src=\"/assets/a.js\"></script>"}`)},
		"dist/content.json": {Data: []byte(`{}`)},
		"dist/assets/a.js":  {Data: []byte("console.log(1)")},
		"dist/ssr.js": {Data: []byte(`globalThis.startRender = (input, ...texts) => {
	globalThis.renderResult = '<!DOCTYPE html><p>` + mark + ` ' + texts.join('|') + '</p>';
};`)},
	}
}

// liveTestVMs is the number of VMs the pool that StartTest keeps holds now.
func liveTestVMs() int {
	tests.mu.Lock()
	defer tests.mu.Unlock()
	if tests.pool == nil {
		return 0
	}
	return tests.pool.Stats().VMs
}

// The VMs of the test servers stay at testVMs in the process, whatever bundles the servers of
// its tests render with one after the other.
func TestStartTestKeepsTheVMsOfTheProcessAtTheCap(t *testing.T) {
	for _, mark := range []string{"one", "two", "three", "one"} {
		app := App{
			Dist: testBundle(mark),
			Routes: []Route{{ID: "app/route", Path: "/", Page: true, SSR: true, Layouts: []string{},
				Load: func(c *tifa.Ctx) (any, error) {
					return tifa.Use(c, func(*tifa.Ctx) (string, error) { return "hi", nil })
				}}},
		}
		srv := httptest.NewUnstartedServer(nil)
		stop, err := StartTest(context.Background(), app, srv.Config)
		if err != nil {
			srv.Close()
			t.Fatalf("%s: %v", mark, err)
		}
		srv.Start()
		res, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if err := stop(srv.Close); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "<p>"+mark+` "hi"</p>`) {
			t.Errorf("%s: the page %s", mark, body)
		}
		if n := liveTestVMs(); n > testVMs {
			t.Errorf("after the bundle %s the test servers keep %d VMs, want at most %d", mark, n, testVMs)
		}
	}
}
