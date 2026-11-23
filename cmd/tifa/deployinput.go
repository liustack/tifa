package main

// The inputs of tifa deploy: values that belong to the developer and not to tifa.yaml, which is
// committed. They share one prefix, TIFA_DEPLOY_.
// Each one is read the same way: its flag when it has one, else the environment variable, else
// the line in the .env at the root of the app. Tifa reads that file. The one thing setup writes
// to it is TIFA_DEPLOY_HOST, when deploy.connect cloudflare moves it to the ssh host name, and only
// when the value came from there. It never writes a token.

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
)

// deployInputNames is every name with the prefix TIFA_DEPLOY_ that tifa deploy reads: the inputs,
// the secrets and variables it sets on GitHub, and the marker. A new one is added here.
var deployInputNames = []string{
	deployHostEnv, secretSSHKey, secretKnownHosts, secretCFID, secretCFSecret,
	varTSClient, varTSAudience, varMarker, envPurgeToken, envZoneID, githubTokenEnv,
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
		dist := config.EditDistance(key, name)
		if score < 0.6 && dist > 2 {
			continue
		}
		if best == "" || score > bestScore || (score == bestScore && dist < bestDist) {
			best, bestScore, bestDist = name, score, dist
		}
	}
	return best
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
