package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// WritePlans writes the plans of both visit lengths (4 and 2 page views a visit on average) to dir,
// for the workbench and for the storefront, where the recorder reads them and the replay finds them
// again.
func WritePlans(dir string, seed uint64, visits int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, mean := range []int{4, 2} {
		for _, site := range []string{SiteWorkbench, SiteStore} {
			plans := GeneratePlans(seed, visits, float64(mean))
			if site == SiteStore {
				plans = GenerateStorePlans(seed, visits, float64(mean))
			}
			data, err := json.Marshal(plans)
			if err != nil {
				return err
			}
			if err := os.WriteFile(SitePlansPath(dir, site, mean), data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// traceName is the name of the traces a spec replays. A spec that is a framework with other
// settings (Tifa with a cache off, a Node app with --jitless) sends what the framework's own
// browser visit sent, so it replays the traces of the framework.
func traceName(s Spec) string {
	if s.Framework != "" {
		return s.Framework
	}
	return s.Name
}

// Record plays the plans of dir for a site in a real browser against each framework and writes the
// traces to dir. The recorder is test/compare/recorder (Node with Playwright). Every framework is
// started fresh for each visit length, unpinned: recording is not timed.
func (e Env) Record(ctx context.Context, log io.Writer, dir, site string, servers []string, means []int, workers, limit int) error {
	recorder := filepath.Join(e.Root, "test", "compare", "recorder")
	if err := run(ctx, log, recorder, nil, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	area := "workbench"
	if site == SiteStore {
		area = "store"
	}
	for _, name := range servers {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return fmt.Errorf("no spec named %q", name)
		}
		spec := e.Specs()[i]
		for _, mean := range means {
			server, err := spec.Start(ctx, "")
			if err != nil {
				return err
			}
			args := []string{"exec", "node", "record.mjs", "--base", server.Base, "--plans", SitePlansPath(dir, site, mean), "--out", SiteTracePath(dir, traceName(spec), site, mean), "--workers", strconv.Itoa(workers), "--area", area}
			if limit > 0 {
				args = append(args, "--limit", strconv.Itoa(limit))
			}
			err = run(ctx, log, recorder, nil, "pnpm", args...)
			// A server that stopped during the recording fails every visit after it with a refused
			// connection, and only its own output says why.
			stopped := ""
			select {
			case <-server.exited:
				stopped = fmt.Sprintf("\n%s stopped during the recording:\n%s", name, server.out)
			default:
			}
			server.Stop()
			if err != nil {
				return fmt.Errorf("%s, mean %d: %w%s", name, mean, err, stopped)
			}
		}
	}
	return nil
}

// CrawlStoreLinks opens every page a visit to the storefront can be on in a real browser, against
// Tifa, and writes to out which of the others each page links to (store_links.json). The pages of
// the other frameworks are the same text, so one crawl serves all.
func (e Env) CrawlStoreLinks(ctx context.Context, log io.Writer, out string) error {
	recorder := filepath.Join(e.Root, "test", "compare", "recorder")
	if err := run(ctx, log, recorder, nil, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "store-links")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	pages := filepath.Join(dir, "pages.json")
	data, err := json.Marshal(StoreVisitPages())
	if err != nil {
		return err
	}
	if err := os.WriteFile(pages, data, 0o644); err != nil {
		return err
	}
	i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == "tifa" })
	server, err := e.Specs()[i].Start(ctx, "")
	if err != nil {
		return err
	}
	defer server.Stop()
	return run(ctx, log, recorder, nil, "pnpm", "exec", "node", "record.mjs", "--base", server.Base, "--crawl", pages, "--out", out)
}
