package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// browserUser gives the browser its name only. A pointer receiver reaches the method too.
type browserUser struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type browserMe struct {
	Name string `json:"name"`
}

func (u *browserUser) Browser() browserMe { return browserMe{Name: u.Name} }

type valueBrowser struct {
	Name string `json:"name"`
}

func (u valueBrowser) Browser() browserMe { return browserMe(u) }

func viewOf(t *testing.T, c *Ctx) string {
	t.Helper()
	v, err := BrowserSessionOf(c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBrowserSessionOfASignedOutRequest(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if got := viewOf(t, c); got != `{"signedIn":false}` {
		t.Errorf("got %s", got)
	}
}

func TestBrowserSessionHasDataFromBrowser(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if err := SetSession(c, &browserUser{ID: 1, Name: "Ann", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	if got := viewOf(t, c); got != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("got %s", got)
	}
}

func TestBrowserSessionWithoutBrowserMethodHasNoData(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if err := SetSession(c, &testSession{UserID: 1, Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if got := viewOf(t, c); got != `{"signedIn":true}` {
		t.Errorf("got %s", got)
	}
}

func TestBrowserMethodOnTheValueType(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if err := SetSession(c, &valueBrowser{Name: "Bo"}); err != nil {
		t.Fatal(err)
	}
	if got := viewOf(t, c); got != `{"signedIn":true,"data":{"name":"Bo"}}` {
		t.Errorf("got %s", got)
	}
}

type lateUser struct {
	Name string `json:"name"`
	Pass string `json:"pass"`
}

func (u lateUser) Browser() browserMe { return browserMe{Name: "late " + u.Name} }

// A request that holds a record, before any code of the request has read the session as S, gets
// the data of the registered type (the generated code of the app does that when it starts).
func TestBrowserSessionOfALoadedRecordUsesTheRegisteredType(t *testing.T) {
	store := newLogStore(t)
	token := strings.Repeat("D", 43)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	store.MemoryStore.Put(context.Background(), hashOf(token), []byte(`{"v":1,"expires":"`+future+`","data":{"name":"Ann","pass":"x"}}`), time.Now().Add(time.Hour))
	RegisterSessionType[lateUser]()
	c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if got := viewOf(t, c); got != `{"signedIn":true,"data":{"name":"late Ann"}}` {
		t.Errorf("got %s", got)
	}
}

func TestBrowserSessionAfterClearIsSignedOut(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if err := SetSession(c, &browserUser{Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if err := ClearSession(c); err != nil {
		t.Fatal(err)
	}
	if got := viewOf(t, c); got != `{"signedIn":false}` {
		t.Errorf("got %s", got)
	}
}

func TestSessionChanged(t *testing.T) {
	c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if SessionChanged(c) {
		t.Fatal("changed before any call")
	}
	if err := SetSession(c, &testSession{Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if !SessionChanged(c) {
		t.Error("not changed after SetSession")
	}
	out, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	if err := ClearSession(out); err != nil {
		t.Fatal(err)
	}
	if !SessionChanged(out) {
		t.Error("not changed after ClearSession of a request with no session: the browser may still hold a copy")
	}
	refused, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
	SetSessionPhase(refused, PhaseError)
	if SetSession(refused, &testSession{}) == nil || SessionChanged(refused) {
		t.Error("a refused call counts as a change")
	}
}

// A request that holds a session, in a process that knows no session type, is an error and not a
// signed-in visitor without data: the browser would otherwise keep a copy without the name.
func TestBrowserSessionWithoutARegisteredTypeIsAnError(t *testing.T) {
	store := newLogStore(t)
	token := strings.Repeat("E", 43)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	store.MemoryStore.Put(context.Background(), hashOf(token), []byte(`{"v":1,"expires":"`+future+`","data":{"name":"Ann"}}`), time.Now().Add(time.Hour))
	c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	sessionKinds.Clear()
	lastKind.Store(nil)
	if _, err := BrowserSessionOf(c); err == nil || !strings.Contains(err.Error(), "session type") {
		t.Errorf("err %v", err)
	}
	// A signed-out request needs no type.
	out, _ := sessionCtx(t, prodEnv(store), "")
	if v, err := BrowserSessionOf(out); err != nil || v.SignedIn {
		t.Errorf("signed out: %v %v", v, err)
	}
}
