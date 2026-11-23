package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A TIFA_DEPLOY_ line in the .env of the app that tifa deploy does not read is a mistake, since the
// prefix is Tifa's. The error names the key and the closest name, and never prints a value.
func TestUnknownDeployNameInDotenvIsAnError(t *testing.T) {
	check := func(t *testing.T, dotenv string) error {
		t.Helper()
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".env"), dotenv)
		return checkUnknownInputNames(root)
	}
	t.Run("old name with the new prefix", func(t *testing.T) {
		err := check(t, "TIFA_DEPLOY_CF_CACHE_PURGE_TOKEN=secret-value\n")
		if err == nil {
			t.Fatal("accepted")
		}
		want := ".env sets TIFA_DEPLOY_CF_CACHE_PURGE_TOKEN, which tifa deploy does not read: did you mean TIFA_DEPLOY_CF_PURGE_TOKEN?"
		if err.Error() != want {
			t.Errorf("got %q, want %q", err, want)
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Errorf("%q has the value", err)
		}
	})
	t.Run("a typo and reordered words", func(t *testing.T) {
		for key, want := range map[string]string{
			"TIFA_DEPLOY_HOSTS":          deployHostEnv,
			"TIFA_DEPLOY_PURGE_CF_TOKEN": envPurgeToken,
			"TIFA_DEPLOY_CF_ZONEID":      envZoneID,
			"TIFA_DEPLOY_GITHUB_TOKENS":  githubTokenEnv,
		} {
			err := check(t, key+"=v\n")
			if err == nil || !strings.Contains(err.Error(), "did you mean "+want+"?") {
				t.Errorf("%s: %v", key, err)
			}
		}
	})
	t.Run("no clear match lists the names", func(t *testing.T) {
		err := check(t, "TIFA_DEPLOY_BANANA=v\n")
		if err == nil || strings.Contains(err.Error(), "did you mean") {
			t.Fatalf("got %v", err)
		}
		for _, n := range deployInputNames {
			if !strings.Contains(err.Error(), n) {
				t.Errorf("%q lacks %s", err, n)
			}
		}
	})
	t.Run("known and unrelated names pass", func(t *testing.T) {
		dotenv := "DATABASE_URL=postgres://x\n"
		for _, n := range deployInputNames {
			dotenv += n + "=v\n"
		}
		if err := check(t, dotenv); err != nil {
			t.Errorf("got %v", err)
		}
	})
	t.Run("the environment is left alone", func(t *testing.T) {
		t.Setenv("TIFA_DEPLOY_CF_CACHE_PURGE_TOKEN", "x")
		if err := check(t, ""); err != nil {
			t.Errorf("got %v", err)
		}
	})
}
