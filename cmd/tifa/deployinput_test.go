package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// An input under its old name in the .env of the app, with the new name missing, stops every
// deploy command and names the new one. It is no silent fallback and no warning.
func TestOldInputNamesAreErrors(t *testing.T) {
	for _, tc := range []struct{ old, new string }{
		{"DEPLOY_HOST", "TIFA_DEPLOY_HOST"},
		{"CF_CACHE_PURGE_TOKEN", "TIFA_DEPLOY_CF_PURGE_TOKEN"},
		{"CF_ZONE_ID", "TIFA_DEPLOY_CF_ZONE_ID"},
	} {
		t.Run(tc.old, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
			for _, n := range []string{"TIFA_DEPLOY_HOST", "TIFA_DEPLOY_CF_PURGE_TOKEN", "TIFA_DEPLOY_CF_ZONE_ID", "DEPLOY_HOST", "CF_CACHE_PURGE_TOKEN", "CF_ZONE_ID"} {
				t.Setenv(n, "")
			}
			writeFile(t, filepath.Join(root, ".env"), tc.old+"=vps\n")
			{
				// The flag gives the server, so only the other two inputs are in question then.
				hostFlag := ""
				if tc.old != "DEPLOY_HOST" {
					hostFlag = "vps"
				}
				_, err := loadDeployTarget(root, hostFlag)
				if err == nil {
					t.Fatalf("%s was accepted", tc.old)
				}
				for _, want := range []string{tc.old, tc.new, "rename"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%q lacks %q", err, want)
					}
				}
				if tc.old == "DEPLOY_HOST" {
					if _, err := loadDeployTarget(root, "vps"); err != nil {
						t.Errorf("--host gives the server, the old name is left alone: %v", err)
					}
				}
				// Once the new name is there too, the old one is only leftover text.
				t.Setenv(tc.new, "vps")
				t.Setenv(deployHostEnv, "vps")
				if _, err := loadDeployTarget(root, ""); err != nil {
					t.Errorf("both names set: %v", err)
				}
			}
		})
	}
}

// The old names are common: other tools read CF_ZONE_ID and DEPLOY_HOST from the environment of a
// shell or a CI job. Only the .env of the app, which earlier versions of Tifa told people to
// write, is checked for them.
func TestOldInputNamesInTheEnvironmentAreLeftAlone(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	for _, n := range []string{"TIFA_DEPLOY_CF_PURGE_TOKEN", "TIFA_DEPLOY_CF_ZONE_ID"} {
		t.Setenv(n, "")
	}
	t.Setenv("TIFA_DEPLOY_HOST", "vps")
	t.Setenv("DEPLOY_HOST", "other")
	t.Setenv("CF_CACHE_PURGE_TOKEN", "x")
	t.Setenv("CF_ZONE_ID", "y")
	if _, err := loadDeployTarget(root, ""); err != nil {
		t.Errorf("old names in the environment stopped the deploy: %v", err)
	}
}

// A TIFA_DEPLOY_ line in the .env of the app that tifa deploy does not read is a mistake, since the
// prefix is Tifa's. The error names the key and the closest name, and never prints a value.
func TestUnknownDeployNameInDotenvIsAnError(t *testing.T) {
	check := func(t *testing.T, dotenv string) error {
		t.Helper()
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".env"), dotenv)
		return checkOldInputNames(root, "")
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
