package main

import (
	"strings"
	"testing"
)

// Setup records the server after it ran: the host that worked, and the app. A second run changes
// nothing, and remove takes the app, and with it the server, off the file.
func TestSetupRemembersTheServer(t *testing.T) {
	homeOf(t)
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, "appdir yes\n"))
	t.Setenv(deployHostEnv, "ssh://203.0.113.7:22")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := readServers()
	if err != nil || len(got) != 1 || got[0].ID != "vps-1234" || got[0].Host != "ssh://203.0.113.7:22" || strings.Join(got[0].Apps, ",") != "shop" {
		t.Fatalf("recorded %+v, %v", got, err)
	}
	if !strings.Contains(out, "Remembered vps (") || !strings.Contains(out, "config.yaml") {
		t.Errorf("no line about it:\n%s", out)
	}

	// Setup with no server named finds it in the file, and the plan says where it came from.
	t.Setenv(deployHostEnv, "")
	out, errOut, err := runSetupIO(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(errOut, "shop is set up on vps") || strings.Contains(out, "Remembered") {
		t.Fatalf("%v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	newSetupServer(t, "", strings.Replace(factsOf("shop", 9531, "appdir yes\n"), "unit-service "+sha(serviceUnit("shop")), "unit-service 00", 1))
	out, _ = runSetup(t, root, false, "")
	if !strings.Contains(out, "Plan for shop on ssh://203.0.113.7:22 (from ") || !strings.Contains(out, "config.yaml)") {
		t.Errorf("the plan does not say where the server came from:\n%s", out)
	}

	// Remove drops the app, and the entry with the last app.
	if out, _, err := runRemove(t, root, false, "", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := readServers(); len(got) != 0 {
		t.Errorf("after remove: %+v", got)
	}
}

// Setup that stops on a failed step records nothing.
func TestSetupFailedRecordsNothing(t *testing.T) {
	homeOf(t)
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	if _, err := runSetup(t, root, false, ""); err == nil {
		t.Fatal("a plan nobody agreed to ran")
	}
	if got, _ := readServers(); len(got) != 0 {
		t.Errorf("recorded %+v", got)
	}
}
