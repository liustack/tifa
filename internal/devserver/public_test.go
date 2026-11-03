package devserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The generated code lists the public files, so one that appears or goes needs a new bundle, which
// writes the code again. Changing the content of one builds nothing.
func TestRunBundlesWhenAPublicFileAppearsOrGoes(t *testing.T) {
	d := startDev(t, "", func(p *buildPipeline) { p.public = "public" })
	d.waitVersionAfter(t, "")
	expectSteps(t, d.p, "Analyze", "Generate", "Bundle", "GoBuild")

	writeFile(t, d.dir, "public/favicon.svg", "<svg/>")
	waitSteps(t, d.p, "Bundle")

	// The same file with other content and a file outside the folder build nothing.
	writeFile(t, d.dir, "public/favicon.svg", "<svg>edited</svg>")
	writeFile(t, d.dir, "static/favicon.svg", "<svg/>")
	time.Sleep(300 * time.Millisecond)
	expectSteps(t, d.p)

	if err := os.Remove(filepath.Join(d.dir, "public", "favicon.svg")); err != nil {
		t.Fatal(err)
	}
	waitSteps(t, d.p, "Bundle")
}

// A change of build.public in tifa.yaml moves the folder tifa dev watches and plans with: files of
// the new folder rebundle, files of the old one no longer do.
func TestRunFollowsAChangeOfThePublicFolder(t *testing.T) {
	d := startDev(t, "", func(p *buildPipeline) { p.public = "public" })
	d.waitVersionAfter(t, "")
	expectSteps(t, d.p, "Analyze", "Generate", "Bundle", "GoBuild")

	d.p.setPublic("static")
	writeFile(t, d.dir, "tifa.yaml", "build:\n  public: static\n")
	waitSteps(t, d.p, "Analyze", "Generate", "Bundle", "GoBuild")

	writeFile(t, d.dir, "static/logo.svg", "<svg/>")
	waitSteps(t, d.p, "Bundle")

	writeFile(t, d.dir, "public/logo.svg", "<svg/>")
	time.Sleep(300 * time.Millisecond)
	expectSteps(t, d.p)
}

// waitSteps waits until the pipeline has run steps since the last look, then checks that they
// are want. A bundle alone keeps the version, so there is no version to wait for.
func waitSteps(t *testing.T, p *buildPipeline, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		n := len(p.calls)
		p.mu.Unlock()
		if n >= len(want) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	expectSteps(t, p, want...)
}

func TestPlanPublicFiles(t *testing.T) {
	cases := []struct {
		name   string
		public string
		change change
		want   steps
	}{
		{"new file", "public", change{"public/a.svg", created}, steps{bundle: true}},
		{"new file deep", "public", change{"public/img/a.png", created}, steps{bundle: true}},
		{"removed file", "public", change{"public/a.svg", removed}, steps{bundle: true}},
		{"edited file", "public", change{"public/a.svg", modified}, steps{}},
		{"new file of another folder", "public", change{"static/a.svg", created}, steps{}},
		{"folder named like the start", "public", change{"public2/a.svg", created}, steps{}},
		{"a folder below the root", "web/static", change{"web/static/a.svg", created}, steps{bundle: true}},
		{"no public folder", "", change{"public/a.svg", created}, steps{}},
		// A script in the folder is still a front end file, with a bundle of its own.
		{"edited script", "public", change{"public/a.js", modified}, steps{bundle: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planPublic([]change{tc.change}, nil, tc.public); got != tc.want {
				t.Errorf("planned %+v, want %+v", got, tc.want)
			}
		})
	}
}
