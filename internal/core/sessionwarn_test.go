package core

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func signInAt(t *testing.T, env *SessionEnv, target string, header map[string]string) {
	t.Helper()
	r := httptest.NewRequest("POST", target, nil)
	for k, v := range header {
		r.Header.Set(k, v)
	}
	c := New(httptest.NewRecorder(), r, "/", 1<<20)
	if err := AttachSession(c, env); err != nil {
		t.Fatal(err)
	}
	if err := SetSession(c, &testSession{UserID: 1}); err != nil {
		t.Fatal(err)
	}
}

func warnEnv(t *testing.T) *SessionEnv {
	e := prodEnv(newLogStore(t))
	e.WarnInsecure = true
	return e
}

func TestSetSessionWarnsOnceWhenTheBrowserWillDropTheCookie(t *testing.T) {
	buf := captureLog(t)
	env := warnEnv(t)
	signInAt(t, env, "http://203.0.113.5:9527/", nil)
	signInAt(t, env, "http://203.0.113.5:9527/", nil)
	got := buf.String()
	if strings.Count(got, "level=WARN") != 1 || !strings.Contains(got, "drop the session cookie") {
		t.Fatalf("log %q", got)
	}
}

func TestSetSessionDoesNotWarnWhenTheCookieSurvives(t *testing.T) {
	for name, tc := range map[string]struct {
		target string
		header map[string]string
	}{
		"https":         {"https://shop.example.com/", nil},
		"localhost":     {"http://localhost:9527/", nil},
		"loopback ip":   {"http://127.0.0.1:9527/", nil},
		"loopback ipv6": {"http://[::1]:9527/", nil},
		"proxy https":   {"http://shop.example.com/", map[string]string{"X-Forwarded-Proto": "https"}},
		"forwarded":     {"http://shop.example.com/", map[string]string{"Forwarded": "for=1.2.3.4;proto=https"}},
	} {
		t.Run(name, func(t *testing.T) {
			buf := captureLog(t)
			signInAt(t, warnEnv(t), tc.target, tc.header)
			if buf.Len() != 0 {
				t.Fatalf("log %q", buf)
			}
		})
	}
}

func TestSetSessionDoesNotWarnWithoutSecureOrWhenNotAsked(t *testing.T) {
	buf := captureLog(t)
	dev := &SessionEnv{Store: newLogStore(t), Cookie: "tifa_session", MaxAge: time.Hour, WarnInsecure: true}
	signInAt(t, dev, "http://203.0.113.5:9527/", nil)
	signInAt(t, prodEnv(newLogStore(t)), "http://203.0.113.5:9527/", nil)
	if buf.Len() != 0 {
		t.Fatalf("log %q", buf)
	}
}
