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

// WritePlans writes a set of plans (blocks blocks of BlockVisits visits) to dir, where the
// recorder reads it and the replay finds it again.
func WritePlans(dir string, seed uint64, blocks int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(GeneratePlanSet(seed, blocks))
	if err != nil {
		return err
	}
	return os.WriteFile(PlansPath(dir), data, 0o644)
}

// ReadPlans reads the plans of dir.
func ReadPlans(dir string) (*PlanSet, error) {
	data, err := os.ReadFile(PlansPath(dir))
	if err != nil {
		return nil, err
	}
	var set PlanSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	if len(set.Plans) != set.Blocks*BlockVisits {
		return nil, fmt.Errorf("%s holds %d plans, %d blocks need %d", PlansPath(dir), len(set.Plans), set.Blocks, set.Blocks*BlockVisits)
	}
	return &set, nil
}

// Record plays the plans of dir in a real browser against each framework and writes the traces to
// dir. The recorder is test/compare/recorder (Node with Playwright). Every framework is started
// fresh, unpinned: recording is not timed.
func (e Env) Record(ctx context.Context, log io.Writer, dir string, servers []string, workers, limit int) error {
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
		server, err := spec.Start(ctx, "")
		if err != nil {
			return err
		}
		args := []string{"exec", "node", "record.mjs", "--base", server.Base, "--plans", PlansPath(dir), "--out", TracePath(dir, name), "--workers", strconv.Itoa(workers)}
		if limit > 0 {
			args = append(args, "--limit", strconv.Itoa(limit))
		}
		err = run(ctx, log, recorder, nil, "pnpm", args...)
		server.Stop()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
