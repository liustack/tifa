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
// where the recorder reads them and the replay finds them again.
func WritePlans(dir string, seed uint64, visits int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, mean := range []int{4, 2} {
		data, err := json.Marshal(GeneratePlans(seed, visits, float64(mean)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(PlansPath(dir, mean), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Record plays the plans of dir in a real browser against each framework and writes the traces to
// dir. The recorder is test/compare/recorder (Node with Playwright). Every framework is started
// fresh for each visit length, unpinned: recording is not timed.
func (e Env) Record(ctx context.Context, log io.Writer, dir string, servers []string, means []int, workers, limit int) error {
	recorder := filepath.Join(e.Root, "test", "compare", "recorder")
	if err := run(ctx, log, recorder, nil, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
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
			args := []string{"exec", "node", "record.mjs", "--base", server.Base, "--plans", PlansPath(dir, mean), "--out", TracePath(dir, name, mean), "--workers", strconv.Itoa(workers)}
			if limit > 0 {
				args = append(args, "--limit", strconv.Itoa(limit))
			}
			err = run(ctx, log, recorder, nil, "pnpm", args...)
			server.Stop()
			if err != nil {
				return fmt.Errorf("%s, mean %d: %w", name, mean, err)
			}
		}
	}
	return nil
}
