package ssr

import (
	"context"
	"encoding/json"
	"github.com/liustack/tifa/internal/pages"
	"github.com/liustack/tifa/internal/workspace"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T, url string) (*Bundle, pages.Snapshot) {
	t.Helper()
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	l, err := pages.New(b.Routes, b.Build.BuildID, workspace.Providers(workspace.NewStore()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := l.Load(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	return b, s
}

func TestActualReactSSRIncludesReadyData(t *testing.T) {
	b, s := fixture(t, "/projects/studio/orders/SH-1042")
	r := New(b, 2, 3*time.Second)
	result, err := r.Render(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"SH-1042", "林夏", "晨间手冲壶", "山海工作室", "data-tsr-stream-part", "正在准备操作"} {
		if !strings.Contains(result.HTML, text) {
			t.Errorf("missing %q in SSR", text)
		}
	}
	if strings.Contains(b.code, "document.location.origin") {
		t.Fatal("browser-only implementation leaked into server bundle")
	}
	t.Logf("VM init + bundle: %s, render: %s", result.Init, result.Render)
}

func TestVMExposesNoIO(t *testing.T) {
	b, _ := fixture(t, "/projects/studio")
	code := b.code + `; globalThis.startRender = () => { const blocked = ['fetch','XMLHttpRequest','WebSocket','localStorage','sessionStorage','indexedDB','document','window','require','process','std','os','setTimeout']; globalThis.renderResult = JSON.stringify({html: JSON.stringify(blocked.filter(k => typeof globalThis[k] !== 'undefined'))}); };`
	r := New(&Bundle{code: code}, 1, 3*time.Second)
	result, err := r.Render(context.Background(), pages.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if result.HTML != "[]" {
		t.Fatalf("unexpected host capabilities: %s", result.HTML)
	}
}

func TestSSRDataEscapesScriptClosingTags(t *testing.T) {
	b, s := fixture(t, "/projects/studio/orders/SH-1042")
	order := s.Scopes["page:projects/[projectId]/orders/[orderId]"]["order"].(workspace.Order)
	order.Customer = `</script><script id="injected">alert(1)</script>`
	s.Scopes["page:projects/[projectId]/orders/[orderId]"]["order"] = order
	result, err := New(b, 1, 3*time.Second).Render(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.HTML, `<script id="injected">`) {
		t.Fatal("unescaped script in HTML or dehydrated state")
	}
}

func TestEachRequestHasItsOwnVM(t *testing.T) {
	code := `globalThis.startRender = input => { globalThis.secret = JSON.parse(input).title; Promise.resolve().then(() => globalThis.renderResult = JSON.stringify({html: globalThis.secret})); };`
	r := New(&Bundle{code: code}, 2, time.Second)
	var wg sync.WaitGroup
	for _, name := range []string{"alice", "bob", "carol", "dave"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.Render(context.Background(), pages.Snapshot{Title: name})
			if err != nil {
				t.Error(err)
				return
			}
			if result.HTML != name {
				t.Errorf("leaked data: want %s, got %s", name, result.HTML)
			}
		}()
	}
	wg.Wait()
}

func TestInfiniteRenderIsInterrupted(t *testing.T) {
	for _, code := range []string{
		`globalThis.startRender = () => { while(true){} };`,
		`globalThis.startRender = () => { Promise.resolve().then(() => { while(true){} }); };`,
	} {
		start := time.Now()
		_, err := New(&Bundle{code: code}, 1, 40*time.Millisecond).Render(context.Background(), pages.Snapshot{})
		if err == nil {
			t.Fatal("unbounded render was accepted")
		}
		if time.Since(start) > time.Second {
			t.Fatal("render deadline was not enforced")
		}
	}
}

func TestUnresolvedPromiseCannotHangRender(t *testing.T) {
	code := `globalThis.startRender = async () => { await new Promise(() => {}); };`
	_, err := New(&Bundle{code: code}, 1, time.Second).Render(context.Background(), pages.Snapshot{})
	if err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidSnapshotFailsBeforeJS(t *testing.T) {
	snapshot := pages.Snapshot{Scopes: map[string]map[string]any{"bad": {"function": func() {}}}}
	_, err := New(&Bundle{code: ""}, 1, time.Second).Render(context.Background(), snapshot)
	var unsupported *json.UnsupportedTypeError
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("got %v, expected %T", err, unsupported)
	}
}
