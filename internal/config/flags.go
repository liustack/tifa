package config

import (
	"fmt"

	"github.com/spf13/pflag"
)

// AddFlags lists -c and the flags of the runtime settings of tifa and of the app in fs, so
// that the command line parses around them and --help shows them. Their values are not read
// from fs: every setting is read from the Sources, see Load. The caller adds the flags that are
// no settings, and fs refuses the rest.
func AddFlags(fs *pflag.FlagSet) {
	fs.StringP("config", "c", "", "Deployment config file (YAML), read over tifa.yaml")
	d := Default()
	for _, f := range runtimeFields(&d) {
		addFlag(fs, FlagName(f.key), f.value.kind(), fmt.Sprintf("%s (default %s)", f.help, f.value))
	}
	// A retired flag is still accepted, so that a start script that has it goes on: Sources
	// reports it as retired.
	for _, r := range retiredKeys {
		if r.projectOnly {
			continue
		}
		kind := "string"
		if r.bool {
			kind = "bool"
		}
		addFlag(fs, FlagName(r.key), kind, "retired, no longer used")
		fs.MarkHidden(FlagName(r.key))
	}
	for _, s := range AppSettings() {
		addFlag(fs, FlagName(s.Key), s.Kind, s.Help)
	}
}

func addFlag(fs *pflag.FlagSet, name, kind, help string) {
	fs.Var(&rawValue{typ: kind}, name, help)
	if kind == "bool" {
		fs.Lookup(name).NoOptDefVal = "true"
	}
}
