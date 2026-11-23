package main

// The inputs of tifa deploy: values that belong to the developer and not to tifa.yaml, which is
// committed. They share one prefix, TIFA_DEPLOY_. Earlier versions named three of them otherwise.
// Each one is read the same way: its flag when it has one, else the environment variable, else
// the line in the .env at the root of the app. Tifa reads that file. The one thing setup writes
// to it is TIFA_DEPLOY_HOST, when deploy.ssh cloudflare moves it to the ssh host name, and only
// when the value came from there. It never writes a token.

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
)

// oldInputNames maps the old name of an input to its new one.
var oldInputNames = []struct{ old, new string }{
	{"DEPLOY_HOST", deployHostEnv},
	{"CF_CACHE_PURGE_TOKEN", envPurgeToken},
	{"CF_ZONE_ID", envZoneID},
}

// deployInputNames is every name with the prefix TIFA_DEPLOY_ that tifa deploy reads: the inputs,
// the secrets and variables it sets on GitHub, and the marker. A new one is added here.
var deployInputNames = []string{
	deployHostEnv, secretSSHKey, secretKnownHosts, secretCFID, secretCFSecret, secretTSID,
	secretTSSecret, varMarker, envPurgeToken, envZoneID, githubTokenEnv,
}

const deployPrefix = "TIFA_DEPLOY_"

// checkUnknownInputNames fails when the .env at root sets a name with the prefix TIFA_DEPLOY_ that
// is not in deployInputNames. The prefix is Tifa's, so such a line is a mistake that would
// otherwise be ignored without a word. The error names the key and the closest name, never a value.
func checkUnknownInputNames(root string) error {
	for _, key := range config.Read(nil, nil, root).DotenvKeys() {
		if !strings.HasPrefix(key, deployPrefix) || slices.Contains(deployInputNames, key) {
			continue
		}
		if near := closestInputName(key); near != "" {
			return fmt.Errorf(".env sets %s, which tifa deploy does not read: did you mean %s?", key, near)
		}
		return fmt.Errorf(".env sets %s, which tifa deploy does not read: it reads %s", key, strings.Join(deployInputNames, ", "))
	}
	return nil
}

// closestInputName returns the name in deployInputNames that key most likely meant, or "" when
// none is clear: the same words in another order or with a few words more or less (at least
// three in five of the words shared), or a typo of two letters or fewer.
func closestInputName(key string) string {
	words := func(s string) []string { return strings.Split(strings.TrimPrefix(s, deployPrefix), "_") }
	best, bestScore, bestDist := "", 0.0, 0
	for _, name := range deployInputNames {
		a, b := words(key), words(name)
		shared := 0
		for _, w := range a {
			if slices.Contains(b, w) {
				shared++
			}
		}
		score := float64(shared) / float64(len(a)+len(b)-shared)
		dist := editDistance(key, name)
		if score < 0.6 && dist > 2 {
			continue
		}
		if best == "" || score > bestScore || (score == bestScore && dist < bestDist) {
			best, bestScore, bestDist = name, score, dist
		}
	}
	return best
}

// editDistance is the number of single-letter changes that turn a into b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// checkOldInputNames fails when an input is still set under its old name in the .env at root, and
// the new name is set nowhere. Reading the old name would work, but it would hide that CI and a
// second machine need the new one, so the error names the new name. Only .env is looked at for
// the old names: earlier versions told people to write them there, while in the environment
// they are common names that other tools set (CF_ZONE_ID, DEPLOY_HOST). hostFlag is the value of
// --host: it gives the server, so DEPLOY_HOST is left alone then.
func checkOldInputNames(root, hostFlag string) error {
	if err := checkUnknownInputNames(root); err != nil {
		return err
	}
	src := config.Read(nil, os.Environ(), root)
	dotenv := config.Read(nil, nil, root)
	for _, n := range oldInputNames {
		if n.new == deployHostEnv && hostFlag != "" {
			continue
		}
		if value, _ := src.Lookup(n.new); value != "" {
			continue
		}
		if value, from := dotenv.Lookup(n.old); value != "" {
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
