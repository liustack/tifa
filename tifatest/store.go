package tifatest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// TestSessionStore runs the checks every tifa.SessionStore must pass against store, which should
// be empty and not shared with anything else. Call it from a test of the store you wrote for
// tifa.SetSessionStore, such as one on Redis or SQL. It reports what fails with t, and takes
// about half a second, as it waits for a record to expire.
//
// Its keys have the form of the framework's: 43 characters of base64url. It deletes what it writes.
func TestSessionStore(t testing.TB, store tifa.SessionStore) {
	t.Helper()
	ctx := context.Background()
	soon := func() time.Time { return time.Now().Add(time.Hour) }
	// The framework's keys are the SHA-256 of a token in base64url.
	key := func(n string) string {
		sum := sha256.Sum256([]byte("tifatest-" + n))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	defer func() {
		for _, n := range []string{"a", "b", "gone", "slice", "short", "ctx", "swarm"} {
			_ = store.Delete(ctx, key(n))
		}
	}()

	if data, found, err := store.Get(ctx, key("a")); found || err != nil {
		t.Errorf("Get of a key that was never stored: found %v, err %v, data %q: a missing key is found == false with a nil error", found, err, data)
	}
	if err := store.Put(ctx, key("a"), []byte("one"), soon()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if data, found, err := store.Get(ctx, key("a")); err != nil || !found || string(data) != "one" {
		t.Errorf("Get after Put: %q, found %v, err %v", data, found, err)
	}
	if err := store.Put(ctx, key("a"), []byte("two"), soon()); err != nil {
		t.Fatalf("Put over an existing record: %v", err)
	}
	if data, _, _ := store.Get(ctx, key("a")); string(data) != "two" {
		t.Errorf("Get after a second Put: %q, want the second record", data)
	}
	if data, found, _ := store.Get(ctx, key("b")); found {
		t.Errorf("Get of another key found %q", data)
	}

	if err := store.Delete(ctx, key("gone")); err != nil {
		t.Errorf("Delete of a key that does not exist: %v, want nil", err)
	}
	store.Put(ctx, key("gone"), []byte("x"), soon())
	if err := store.Delete(ctx, key("gone")); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if _, found, err := store.Get(ctx, key("gone")); found || err != nil {
		t.Errorf("Get after Delete: found %v, err %v", found, err)
	}

	// A record whose expiry has passed is not found, also when it was written already expired.
	if err := store.Put(ctx, key("short"), []byte("x"), time.Now().Add(250*time.Millisecond)); err != nil {
		t.Errorf("Put with a short expiry: %v", err)
	}
	store.Put(ctx, key("gone"), []byte("x"), time.Now().Add(-time.Second))
	if _, found, err := store.Get(ctx, key("gone")); found || err != nil {
		t.Errorf("Get of a record put with an expiry in the past: found %v, err %v, want an expired record not found", found, err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, found, err := store.Get(ctx, key("short")); found || err != nil {
		t.Errorf("Get after the expiry: found %v, err %v, want an expired record not found with a nil error", found, err)
	}

	// The store neither keeps the slice it is given nor hands out the one it uses.
	data := []byte("original")
	store.Put(ctx, key("slice"), data, soon())
	copy(data, "CHANGED!")
	got, _, _ := store.Get(ctx, key("slice"))
	if !bytes.Equal(got, []byte("original")) {
		t.Errorf("Put kept the slice of the caller: Get returned %q after the caller changed it", got)
	}
	copy(got, "MUTATED!")
	again, _, _ := store.Get(ctx, key("slice"))
	if !bytes.Equal(again, []byte("original")) {
		t.Errorf("Get returned a slice the store uses: a change to it showed in the next Get as %q", again)
	}

	// A done context is an error, not a missing record.
	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.Get(done, key("a")); err == nil {
		t.Errorf("Get with a canceled context returned no error")
	}
	if err := store.Put(done, key("ctx"), []byte("x"), soon()); err == nil {
		t.Errorf("Put with a canceled context returned no error")
	}
	if err := store.Delete(done, key("a")); err == nil {
		t.Errorf("Delete with a canceled context returned no error")
	}
	if _, found, _ := store.Get(ctx, key("ctx")); found {
		t.Errorf("Put with a canceled context stored the record")
	}
	if _, found, _ := store.Get(ctx, key("a")); !found {
		t.Errorf("Delete with a canceled context deleted the record")
	}

	// Many requests call the store at once. Run this test with -race to see data races.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				v := []byte(fmt.Sprint("v", i))
				if err := store.Put(ctx, key("swarm"), v, soon()); err != nil {
					t.Errorf("Put at the same time as others: %v", err)
					return
				}
				if _, _, err := store.Get(ctx, key("swarm")); err != nil {
					t.Errorf("Get at the same time as others: %v", err)
					return
				}
				if err := store.Delete(ctx, key("swarm")); err != nil {
					t.Errorf("Delete at the same time as others: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
