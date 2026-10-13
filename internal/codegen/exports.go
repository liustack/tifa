package codegen

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// exportsOf returns the names a TSX module exports, sorted. esbuild parses the file alone without
// bundling, so nothing runs and imports stay unresolved. Names re-exported with export * from
// another module are not seen, the optional exports of route.tsx and layout.tsx must be declared
// or named in the file itself.
func exportsOf(file string) ([]string, error) {
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{file},
		Bundle:      false,
		Write:       false,
		Metafile:    true,
		Format:      api.FormatESModule,
		JSX:         api.JSXAutomatic,
		LogLevel:    api.LogLevelSilent,
	})
	if len(result.Errors) > 0 {
		text := api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return nil, errors.New(strings.TrimSpace(strings.Join(text, "")))
	}
	var meta struct {
		Outputs map[string]struct {
			Exports []string `json:"exports"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(result.Metafile), &meta); err != nil {
		return nil, err
	}
	if len(meta.Outputs) != 1 {
		return nil, errors.New("esbuild returned no single output for " + file)
	}
	for _, out := range meta.Outputs {
		return slices.Sorted(slices.Values(out.Exports)), nil
	}
	panic("unreachable")
}
