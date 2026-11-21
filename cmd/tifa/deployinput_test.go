package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// An input under its old name, with the new name missing, stops every deploy command and names
// the new one. It is no silent fallback and no warning.
func TestOldInputNamesAreErrors(t *testing.T) {
	for _, tc := range []struct{ old, new string }{
		{"DEPLOY_HOST", "TIFA_DEPLOY_HOST"},
		{"CF_CACHE_PURGE_TOKEN", "TIFA_DEPLOY_CF_PURGE_TOKEN"},
		{"CF_ZONE_ID", "TIFA_DEPLOY_CF_ZONE_ID"},
	} {
		for _, where := range []string{"environment", ".env"} {
			t.Run(tc.old+" in "+where, func(t *testing.T) {
				root := t.TempDir()
				writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
				for _, n := range []string{"TIFA_DEPLOY_HOST", "TIFA_DEPLOY_CF_PURGE_TOKEN", "TIFA_DEPLOY_CF_ZONE_ID", "DEPLOY_HOST", "CF_CACHE_PURGE_TOKEN", "CF_ZONE_ID"} {
					t.Setenv(n, "")
				}
				if where == "environment" {
					t.Setenv(tc.old, "vps")
				} else {
					writeFile(t, filepath.Join(root, ".env"), tc.old+"=vps\n")
				}
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
			})
		}
	}
}
