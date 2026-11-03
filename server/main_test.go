package server

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func runApp(t *testing.T, ctx context.Context, app App, environ []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = run(ctx, app, args, environ, &out, &errOut, nil)
	return out.String(), errOut.String(), err
}

func TestConfigCommandPrintsMergedSettings(t *testing.T) {
	app := App{Settings: map[string]string{"server.addr": "127.0.0.1:9000", "server.body_limit": "2MiB"}}
	out, _, err := runApp(t, context.Background(), app, []string{"SERVER_BODY_LIMIT=3MiB", "TIFA_SSR_VMS=9"}, "config", "--ssr-vms", "5", "--app-flag", "x")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, want := range []string{"addr: \"127.0.0.1:9000\"", "From tifa.yaml.", "body_limit: 3MiB", "From env SERVER_BODY_LIMIT.", "vms: 5", "From flag --ssr-vms."} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// The program lists every invalid setting at once.
func TestInvalidSettingsAreAllListed(t *testing.T) {
	_, _, err := runApp(t, context.Background(), App{}, []string{"SSR_TIMEOUT=soon"}, "--ssr-vms", "two", "--server-hsts", "-c", "missing.yaml")
	for _, want := range []string{"env SSR_TIMEOUT", "flag --ssr-vms", "missing.yaml"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %q", err, want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	out, _, err := runApp(t, context.Background(), App{}, nil, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out, "tifa ") {
		t.Fatalf("version output %q", out)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	if _, _, err := runApp(t, context.Background(), App{}, nil, "deploy"); err == nil {
		t.Fatal("an unknown command succeeded")
	}
}

// serve is the default command: it runs OnStart, serves until the context ends and runs OnShutdown.
func TestServeIsTheDefaultCommand(t *testing.T) {
	for _, args := range [][]string{{"--server-addr", "127.0.0.1:0"}, {"serve", "--server-addr", "127.0.0.1:0"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var events []string
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			app := App{Hooks: Hooks{
				OnStart: func(context.Context) error {
					events = append(events, "start")
					cancel()
					return nil
				},
				OnShutdown: func(context.Context) error {
					events = append(events, "shutdown")
					return nil
				},
			}}
			done := make(chan error, 1)
			go func() {
				_, _, err := runApp(t, ctx, app, nil, args...)
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("serve: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("serve did not stop")
			}
			if strings.Join(events, ",") != "start,shutdown" {
				t.Fatalf("events %q", events)
			}
		})
	}
}
