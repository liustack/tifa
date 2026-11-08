package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxSessionRecord is the largest record SetSession stores, the envelope included.
const maxSessionRecord = 4 << 10

// sessionNow is the clock of session expiry. Tests set it.
var sessionNow = time.Now

// SessionEnv is what a request needs to read and write its session. The server builds one from the
// settings and the store, and tifatest builds one around a store of the test.
type SessionEnv struct {
	Store  SessionStore
	Cookie string        // name of the cookie
	Secure bool          // the cookie has the Secure attribute, false under tifa dev
	MaxAge time.Duration // how long a session lasts, from SetSession
}

// Phase says where a request is in the code of the app, which decides whether SetSession and
// ClearSession may run. A Load or a Layout is told apart by the Ctx it gets, not by the phase.
type Phase uint8

const (
	// PhaseRequest is OnRequest before it calls next, and the method handlers inside next.
	PhaseRequest Phase = iota
	// PhaseAfterNext is OnRequest after next returned.
	PhaseAfterNext
	// PhaseError is OnError.
	PhaseError
)

// SetSessionPhase records the phase of the request c belongs to, for every Ctx of the request.
func SetSessionPhase(c *Ctx, p Phase) { c.req.sess.phase.Store(uint32(p)) }

// sessionState is the session of one request, shared by every Ctx of it.
type sessionState struct {
	phase   atomic.Uint32
	noStore atomic.Bool // the answer sets or clears the cookie, or comes from a failed store
	changed atomic.Bool // SetSession or ClearSession ran, see SessionChanged
	// anonymous is set for a request of a page with Cache: it has no session and
	// cannot make one, see SetAnonymous.
	anonymous atomic.Bool

	mu      sync.Mutex // guards what follows, and is held while the store is called
	env     *SessionEnv
	hash    string         // store key of the token the request holds, empty for none
	have    bool           // the request has a session
	data    jsontext.Value // its data, as the envelope holds it
	decoded bool           // value holds data decoded as vtype
	value   any            // *S
	vtype   reflect.Type   // S, set by the first Session or SetSession
}

// SetAnonymous makes the request anonymous: the server dropped its Cookie and Authorization headers
// and read no session, so Session is nil, and SetSession and ClearSession fail. A page with Cache
// answers every visitor alike, so nothing of the visitor may reach it.
func SetAnonymous(c *Ctx) { c.req.sess.anonymous.Store(true) }

// Anonymous reports whether the request was made anonymous, see SetAnonymous.
func Anonymous(c *Ctx) bool { return c.req.sess.anonymous.Load() }

// NoStore reports whether the answer to c must not be stored or turned into a 304: it sets or
// clears the session cookie, or it is the answer to a session store that failed.
func NoStore(c *Ctx) bool { return c.req.sess.noStore.Load() }

type sessionRecord struct {
	V       int            `json:"v"`
	Expires time.Time      `json:"expires"`
	Data    jsontext.Value `json:"data"`
}

// AttachSession reads the session cookie of the request and loads its session from the store.
// A cookie that is not a token is dropped without asking the store. A record the framework cannot
// read is deleted and its cookie dropped. An error means the store failed.
func AttachSession(c *Ctx, env *SessionEnv) error {
	st := &c.req.sess
	st.mu.Lock()
	defer st.mu.Unlock()
	st.env = env
	ck, err := c.r.Cookie(env.Cookie)
	if err != nil {
		return nil
	}
	if !validToken(ck.Value) {
		c.writeSessionCookie("", -1)
		return nil
	}
	st.hash = hashToken(ck.Value)
	data, found, err := env.Store.Get(c.r.Context(), st.hash)
	if err != nil {
		st.noStore.Store(true)
		return fmt.Errorf("tifa: reading the session: %w", err)
	}
	if !found {
		// The record is gone, so the cookie is dead: drop it, or every request would ask the store again.
		c.writeSessionCookie("", -1)
		return nil
	}
	var rec sessionRecord
	switch err := UnmarshalJSON(data, &rec); {
	case err != nil:
		c.dropSession("the record is not valid JSON", err)
	case rec.V != 1:
		c.dropSession(fmt.Sprintf("unknown record version %d", rec.V), nil)
	case rec.Expires.IsZero():
		c.dropSession("the record has no expiry", nil)
	case !sessionNow().Before(rec.Expires):
		c.dropSession("the record has expired", nil)
	case len(rec.Data) == 0 || string(rec.Data) == "null":
		c.dropSession("the record has no data", nil)
	default:
		st.have, st.data = true, rec.Data
	}
	return nil
}

// dropSession deletes the record the request came with, drops its cookie and treats the request as
// signed out. The caller holds the session lock.
func (c *Ctx) dropSession(why string, err error) {
	st := &c.req.sess
	attrs := []any{"why", why, "method", c.r.Method, "path", c.r.URL.Path}
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	Logger().WarnContext(c.r.Context(), "tifa: dropping a session", attrs...)
	if derr := detachedDelete(c.r.Context(), st.env.Store, st.hash); derr != nil {
		Logger().WarnContext(c.r.Context(), "tifa: deleting a dropped session failed", "err", derr)
	}
	// Once the response is out, its headers are final. The record is gone, and the next request
	// finds no record and drops the cookie.
	if !c.committed() {
		c.writeSessionCookie("", -1)
	}
	st.have, st.decoded, st.data, st.value, st.hash = false, false, nil, nil, ""
}

// detachedDelete deletes key even when ctx is done, as the cleanup after a failure must still try.
func detachedDelete(ctx context.Context, store SessionStore, key string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return store.Delete(ctx, key)
}

// writeSessionCookie sets the session cookie, or drops it with a negative maxAge, and replaces the
// session Set-Cookie an earlier call of the request wrote. The caller holds the session lock.
func (c *Ctx) writeSessionCookie(value string, maxAge int) {
	env := c.req.sess.env
	c.req.sess.noStore.Store(true)
	c.req.mu.Lock()
	defer c.req.mu.Unlock()
	h := c.w.Header()
	var keep []string
	for _, v := range h.Values("Set-Cookie") {
		if !strings.HasPrefix(v, env.Cookie+"=") {
			keep = append(keep, v)
		}
	}
	if keep == nil {
		h.Del("Set-Cookie")
	} else {
		h["Set-Cookie"] = keep
	}
	http.SetCookie(c.w, &http.Cookie{
		Name: env.Cookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: env.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// committed reports whether the response is on its way out, which the server's writer tells.
func (c *Ctx) committed() bool {
	w, ok := c.w.(interface{ Committed() bool })
	return ok && w.Committed()
}

// sessionWritable explains why the request cannot change its session now, nil when it can.
func (c *Ctx) sessionWritable(fn string) error {
	st := &c.req.sess
	switch {
	case st.anonymous.Load():
		return fmt.Errorf("tifa: %s cannot be called while a page with Cache is served: that request is anonymous and its answer is kept for everyone. Sign in and OAuth callbacks belong on routes without Cache", fn)
	case st.env == nil:
		return fmt.Errorf("tifa: %s has no session store: the request was not made by the server", fn)
	case c.mode == modeLoad:
		return fmt.Errorf("tifa: %s cannot be called in a Load or a Layout, only in OnRequest before it calls next, or in a method handler", fn)
	case Phase(st.phase.Load()) == PhaseAfterNext:
		return fmt.Errorf("tifa: %s cannot be called in OnRequest after next returned, call it before next", fn)
	case Phase(st.phase.Load()) == PhaseError:
		return fmt.Errorf("tifa: %s cannot be called in OnError", fn)
	case c.committed():
		return fmt.Errorf("tifa: %s cannot be called once the response is sent", fn)
	}
	return nil
}

// SetSession signs the request in: it stores s as the session under a new token, deletes the
// session the request came with, and sets the cookie. See tifa.SetSession.
func SetSession[S any](c *Ctx, s *S) error {
	if err := c.sessionWritable("SetSession"); err != nil {
		return err
	}
	if s == nil {
		return errors.New("tifa: SetSession needs a session, got nil")
	}
	data, err := MarshalJSON(s)
	if err != nil {
		return fmt.Errorf("tifa: encoding the session: %w", err)
	}
	if string(data) == "null" {
		return errors.New("tifa: the session encodes as null")
	}
	RegisterSessionType[S]()
	st := &c.req.sess
	st.mu.Lock()
	defer st.mu.Unlock()
	expires := sessionNow().Add(st.env.MaxAge).UTC().Truncate(time.Second)
	record, err := MarshalJSON(sessionRecord{V: 1, Expires: expires, Data: data})
	if err != nil {
		return fmt.Errorf("tifa: encoding the session: %w", err)
	}
	if len(record) > maxSessionRecord {
		return fmt.Errorf("tifa: the session takes %d bytes, over the limit of 4 KiB: keep an ID in it and the rest in your database", len(record))
	}
	t := reflect.TypeFor[S]()
	if st.vtype != nil && st.vtype != t {
		panic(sessionTypeMismatch(st.vtype, t))
	}
	token := newToken()
	hash := hashToken(token)
	ctx := c.r.Context()
	if err := st.env.Store.Put(ctx, hash, record, expires); err != nil {
		return fmt.Errorf("tifa: storing the session: %w", err)
	}
	if st.hash != "" {
		if err := st.env.Store.Delete(ctx, st.hash); err != nil {
			_ = detachedDelete(ctx, st.env.Store, hash)
			return fmt.Errorf("tifa: the previous session was not revoked, so the sign-in did not complete: %w", err)
		}
	}
	c.writeSessionCookie(token, int(st.env.MaxAge/time.Second))
	st.hash, st.have, st.data = hash, true, data
	st.decoded, st.value, st.vtype = false, nil, t
	st.changed.Store(true)
	return nil
}

// Session returns the session of the request, nil when there is none. See tifa.Session.
func Session[S any](c *Ctx) *S {
	RegisterSessionType[S]()
	st := &c.req.sess
	st.mu.Lock()
	defer st.mu.Unlock()
	t := reflect.TypeFor[S]()
	if st.vtype != nil && st.vtype != t {
		panic(sessionTypeMismatch(st.vtype, t))
	}
	if !st.have {
		return nil
	}
	if st.decoded {
		return st.value.(*S)
	}
	v := new(S)
	if err := UnmarshalJSON(st.data, v); err != nil {
		c.dropSession(fmt.Sprintf("the data does not fit %s", t), err)
		return nil
	}
	st.decoded, st.value, st.vtype = true, v, t
	return v
}

func sessionTypeMismatch(first, second reflect.Type) string {
	return fmt.Sprintf("tifa: the session is used as %s and as %s: use one type for Session and SetSession", first, second)
}

// ClearSession signs the request out: it deletes the record, drops the cookie and makes the
// session of the request nil. See tifa.ClearSession.
func ClearSession(c *Ctx) error {
	if err := c.sessionWritable("ClearSession"); err != nil {
		return err
	}
	st := &c.req.sess
	st.mu.Lock()
	defer st.mu.Unlock()
	hash := st.hash
	st.have, st.decoded, st.data, st.value, st.hash = false, false, nil, nil, ""
	st.changed.Store(true)
	if hash == "" {
		return nil
	}
	err := st.env.Store.Delete(c.r.Context(), hash)
	c.writeSessionCookie("", -1)
	if err != nil {
		return fmt.Errorf("tifa: deleting the session failed, so its record stays valid until it expires: %w", err)
	}
	return nil
}

// InjectSession gives the request the session s without a store or a cookie. For tests.
func InjectSession[S any](c *Ctx, s *S) {
	st := &c.req.sess
	st.mu.Lock()
	defer st.mu.Unlock()
	st.have, st.decoded, st.value, st.vtype, st.data, st.hash = true, true, s, reflect.TypeFor[S](), nil, ""
}

// newToken returns 32 random bytes as 43 base64url characters.
func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("tifa: reading random bytes: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// hashToken is the store key of a token: the SHA-256 in base64url, so a store that leaks holds no
// value a browser could send.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validToken(s string) bool {
	if len(s) != 43 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !(b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_') {
			return false
		}
	}
	return true
}
