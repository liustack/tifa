package main

// The inputs of tifa deploy: values that belong to the developer and not to tifa.yaml, which is
// committed. They share one prefix, TIFA_DEPLOY_. Earlier versions named three of them otherwise.
// Each one is read the same way: its flag when it has one, else the environment variable, else
// the line in the .env at the root of the app. Tifa only reads that file and never writes to it.

import (
	"fmt"
	"os"

	"github.com/liustack/tifa/internal/config"
)

// oldInputNames maps the old name of an input to its new one.
var oldInputNames = []struct{ old, new string }{
	{"DEPLOY_HOST", deployHostEnv},
	{"CF_CACHE_PURGE_TOKEN", envPurgeToken},
	{"CF_ZONE_ID", envZoneID},
}

// checkOldInputNames fails when an input is still set under its old name, in the environment or
// in the .env at root, and the new name is set nowhere. Reading the old name would work, but it
// would hide that CI and a second machine need the new one, so the error names the new name.
// hostFlag is the value of --host: it gives the server, so DEPLOY_HOST is left alone then.
func checkOldInputNames(root, hostFlag string) error {
	src := config.Read(nil, os.Environ(), root)
	for _, n := range oldInputNames {
		if n.new == deployHostEnv && hostFlag != "" {
			continue
		}
		if value, _ := src.Lookup(n.new); value != "" {
			continue
		}
		if value, from := src.Lookup(n.old); value != "" {
			return fmt.Errorf("%s is set, and %s has been renamed to %s: rename it", from, n.old, n.new)
		}
	}
	return nil
}

// readDeployInput finds the input called name for the app at root: flagValue (the flag flagName,
// which an input without a flag leaves empty), else the environment variable, else the .env file
// at root. from says where the value came from, for an error. Both are empty when the input is
// set nowhere.
func readDeployInput(root, flagName, flagValue, name string) (value, from string, err error) {
	if flagValue != "" {
		return flagValue, flagName, nil
	}
	src := config.Read(nil, os.Environ(), root)
	if err := src.Err(); err != nil {
		return "", "", err
	}
	value, from = src.Lookup(name)
	return value, from, nil
}
