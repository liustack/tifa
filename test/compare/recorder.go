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

// WritePlans writes the plans of the site (blocks blocks of SiteBlockPlans visits each) to dir,
// where the recorder reads them and the replay finds them again.
func WritePlans(dir string, seed uint64, blocks int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(GenerateSitePlanSet(seed, blocks))
	if err != nil {
		return err
	}
	return os.WriteFile(PlansPath(dir), data, 0o644)
}

// ReadPlans reads the plans of the site from dir.
func ReadPlans(dir string) (*PlanSet, error) {
	data, err := os.ReadFile(PlansPath(dir))
	if err != nil {
		return nil, err
	}
	var set PlanSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	if len(set.Plans) != set.Blocks*SiteBlockPlans {
		return nil, fmt.Errorf("%s holds %d plans, %d blocks need %d", PlansPath(dir), len(set.Plans), set.Blocks, set.Blocks*SiteBlockPlans)
	}
	if set.Site != SiteShop {
		return nil, fmt.Errorf("%s holds the plans of the site %q, not of %q: make them again", PlansPath(dir), set.Site, SiteShop)
	}
	if err := checkLandingQuotas(&set); err != nil {
		return nil, fmt.Errorf("%s: %w: make the plans again (`compare plans`) and record the traces again (`compare record`)", PlansPath(dir), err)
	}
	return &set, nil
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

// Record plays the plans of dir in a real browser against each framework and writes the
// traces to dir. The recorder is test/compare/recorder (Node with Playwright). Every framework is
// started fresh, unpinned: recording is not timed.
func (e Env) Record(ctx context.Context, log io.Writer, dir string, servers []string, workers, limit int) error {
	// The recorder runs in its own folder, so it gets absolute paths.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	recorder := filepath.Join(e.Root, "test", "compare", "recorder")
	if err := run(ctx, log, recorder, nil, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	names, err := e.recorded(servers)
	if err != nil {
		return err
	}
	for _, name := range names {
		spec, err := e.Target(name)
		if err != nil {
			return err
		}
		server, err := spec.Start(ctx, "")
		if err != nil {
			return err
		}
		args := []string{"exec", "node", "record.mjs", "--base", server.Base, "--plans", PlansPath(dir), "--out", TracePath(dir, name), "--workers", strconv.Itoa(workers)}
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
			return fmt.Errorf("%s: %w%s", name, err, stopped)
		}
	}
	return nil
}

// CrawlStoreLinks opens every page a visit to the storefront can be on in a real browser, against
// Tifa, and writes to out which of the others each page links to (store_links.json). The pages of
// the other frameworks are the same text, so one crawl serves all.
func (e Env) CrawlStoreLinks(ctx context.Context, log io.Writer, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
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

// recorded are the specs a recording of servers records, in order: one for each trace, the spec of
// its framework. A spec that replays the trace of another (Tifa with its render memo off replays the
// trace of Tifa) is recorded through that one, since the browser sends the same requests to both.
func (e Env) recorded(servers []string) ([]string, error) {
	var out []string
	for _, name := range servers {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no spec named %q", name)
		}
		if t := traceName(e.Specs()[i]); !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}
