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

// SetSession signs the request in. It stores s as the session under a new random token, deletes the
// session the request came with, and sets the session cookie, which lasts 30 days (session.max_age)
// from now and is not renewed. Session[S](c) returns the new value for the rest of the request. Every
// call makes a new token, so calling it again after a sign-in changes the token again.
//
// s is encoded as JSON, and the record, with the framework's envelope, must not pass 4 KiB: keep
// an ID and a few display fields in it, and the rest in your database. The record is trusted: a
// store that others can write to can sign anyone in.
//
// It returns an error, and changes nothing, when the store cannot be written. When the old
// session cannot be deleted the sign-in is not done either, so the old session is not left behind
// next to the new one. It also returns an error in a Load, a Layout and OnError, in OnRequest once
// next returned, and after the response is sent. Call it in OnRequest before next, or in a method handler.
//
// S must be the same type in every call of SetSession and Session, or the call panics.
//
// A page with Cache in its route.go is served anonymously: its GET and HEAD requests have no
// session and SetSession returns an error there. Put sign-in and OAuth callbacks on routes
// without Cache.
func SetSession[S any](c *Ctx, s *S) error { return core.SetSession(c, s) }

// Session returns the session of the request, nil when nobody is signed in. The server reads it
// before OnRequest runs, so every hook, Load and method handler can call it, and the Loads that run
// at once share the one value: treat it as read-only. The record is decoded into S on the first call.
// A record that does not fit S is deleted and its cookie dropped, and the request is signed out.
// On a page with Cache in its route.go, GET and HEAD requests are anonymous and Session is nil.
func Session[S any](c *Ctx) *S { return core.Session[S](c) }

// ClearSession signs the request out: it deletes the record, drops the cookie, and Session returns nil
// from now on. It does nothing when the request has no session. When the record cannot be deleted
// the cookie is still dropped, and the error says the record stays valid until it expires.
// Where it may be called is the same as for SetSession.
func ClearSession(c *Ctx) error { return core.ClearSession(c) }
