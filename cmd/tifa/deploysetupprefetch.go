package main

// Planning reads the server, Cloudflare and GitHub, and each read waits for a network round trip.
// The plan code reads one thing after the other, and stays that way: it is easy to follow. Before
// it starts, prefetch asks for what the settings will need on several goroutines, at once. Each
// answer lands in the memo of the plan, and the plan code then finds it there. A prefetch that
// guesses wrong costs one wasted call. It never changes the result: the plan asks again for
// whatever the memo lacks.

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
)

// errNoPrefetch stands for a GitHub that the plan will not ask about, so nothing depends on it.
var errNoPrefetch = errors.New("no GitHub reads")

// lockedWriter lets the goroutines of a plan share a writer.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// prefetch starts the reads of the plan that do not depend on each other. ctx carries the memo.
// Its goroutines join wg, and write nothing of r.
func (r *setupRun) prefetch(ctx context.Context, wg *sync.WaitGroup) {
	t := r.t
	spawn := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}

	// The server.
	stateDone := make(chan struct{})
	var stateErr error
	var st serverState
	spawn(func() {
		defer close(stateDone)
		st, stateErr = r.readState(ctx)
	})

	// GitHub, when the plan will ask: it is wanted, or Tifa wrote the workflow. A key on the
	// server that would also make it ask is known only after the server answered.
	_, marked, _ := hasMarker(filepath.Join(t.root, filepath.FromSlash(githubWorkflowPath)))
	repoDone := make(chan struct{})
	var repo githubRepo
	var repoErr error
	if t.github || marked {
		spawn(func() {
			defer close(repoDone)
			var api githubAPI
			if api, repo, repoErr = findGitHub(ctx, t.root); repoErr != nil {
				return
			}
			spawn(func() { api.SecretNames(ctx, repo.Name) })
			spawn(func() { api.Variables(ctx, repo.Name) })
		})
	} else {
		close(repoDone)
		repoErr = errNoPrefetch
	}

	// Cloudflare, when the app has a domain.
	if t.domain == "" || !haveCF() {
		return
	}
	loggedIn := make(chan bool, 1)
	spawn(func() { loggedIn <- cfLoggedIn(ctx) })
	zoneDone := make(chan struct{})
	var zone cfZone
	var zoneErr error
	spawn(func() {
		defer close(zoneDone)
		zone, zoneErr = findZone(ctx, t.domain)
	})
	spawn(func() {
		<-stateDone
		<-zoneDone
		if !<-loggedIn || stateErr != nil || zoneErr != nil {
			return
		}
		id, _, err := ensureTunnel(ctx, st.TunnelID, "tifa-"+st.Machine, false)
		if err != nil || id == "" {
			return
		}
		target := id + ".cfargotunnel.com"
		spawn(func() { tunnelIngress(ctx, id) })
		spawn(func() { ensureDNS(ctx, zone.Name, t.domain, target, t.app, false) })
		for _, rs := range hostRules(t.domain) {
			spawn(func() { ensureRules(ctx, zone.Name, rs, false) })
		}
		if !t.github || t.sshMode != connCloudflare {
			return
		}
		<-repoDone
		if repoErr != nil {
			return
		}
		name := accessName(repo.Name)
		spawn(func() {
			found, err := findSSHHosts(ctx, id, t.domain, zone.Name)
			if err != nil || found.host == "" {
				return
			}
			spawn(func() { ensureDNS(ctx, zone.Name, found.host, target, t.app, false) })
			spawn(func() { ensureSSHApp(ctx, found.host, "", false) })
		})
		spawn(func() { serviceTokenIDs(ctx, name) })
		spawn(func() { policyByName(ctx, name) })
	})
}
