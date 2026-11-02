package tifa

import "github.com/liustack/tifa/internal/core"

// SessionStore keeps the records of sessions. The framework stores one record per session under
// the SHA-256 of the session token (base64url), not under the token the browser holds. Get, Put
// and Delete are called from many requests at once and must be safe for that.
//
// Get returns the record. A key that is missing, expired or not valid is found == false with a nil
// error. err is only for a failure of the store itself, such as a lost connection: the request
// then fails, it is not treated as signed out. Put creates or replaces the record, and Get does
// not return it after expiry. It must not keep the data slice it is given, and Get must not
// return a slice the store still uses. Delete returns nil for a key that does not exist.
// All three return an error when ctx is done.
type SessionStore = core.SessionStore

// MemoryStore is the SessionStore that keeps records in the memory of the process. A restart signs
// everyone out. NewMemoryStore makes one.
type MemoryStore = core.MemoryStore

// NewMemoryStore returns a store that holds at most maxBytes bytes, counting the key, the data and
// a fixed cost per record. When it is full, a Put drops the records that expire first. A record
// that alone is larger than maxBytes makes Put return an error. It panics when maxBytes is not
// positive. The server uses one of 64 MiB (session.memory_limit) when OnStart sets no store.
func NewMemoryStore(maxBytes int64) *MemoryStore { return core.NewMemoryStore(maxBytes) }

// SetSessionStore sets where sessions are kept, for example a store on Redis or SQL, in place of
// the in-memory one. Call it in OnStart: it panics once the server listens. nil restores the
// in-memory store.
func SetSessionStore(s SessionStore) { core.SetSessionStore(s) }
