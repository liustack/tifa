package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// BrowserSession is the session as the browser gets it: whether anyone is signed in and, when the
// session type has an Exposed method, what it returns.
type BrowserSession struct {
	SignedIn bool            `json:"signedIn"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// sessionKind is what the framework needs to know about the session type S to build a
// BrowserSession from a record: how to read the session as S, and how to call Exposed.
type sessionKind struct {
	t       reflect.Type
	load    func(c *Ctx) any                           // Session[S], nil when there is no session
	exposed func(v any) (json.RawMessage, bool, error) // nil when S has no Exposed method
}

var (
	sessionKinds sync.Map // reflect.Type -> *sessionKind
	lastKind     atomic.Pointer[sessionKind]
)

// RegisterSessionType tells the process the session type S, so that a request which holds a
// session record can be turned into a BrowserSession before any code of the request has read the
// session as S. SetSession and Session register their type too.
func RegisterSessionType[S any]() {
	t := reflect.TypeFor[S]()
	if k := lastKind.Load(); k != nil && k.t == t {
		return
	}
	k, ok := sessionKinds.Load(t)
	if !ok {
		k, _ = sessionKinds.LoadOrStore(t, newKind[S]())
	}
	lastKind.Store(k.(*sessionKind))
}

func newKind[S any]() *sessionKind {
	k := &sessionKind{t: reflect.TypeFor[S](), load: func(c *Ctx) any {
		if s := Session[S](c); s != nil {
			return s
		}
		return nil
	}}
	m, ok := reflect.TypeFor[*S]().MethodByName("Exposed")
	if !ok || m.Type.NumIn() != 1 || m.Type.NumOut() != 1 {
		return k
	}
	k.exposed = func(v any) (data json.RawMessage, ok bool, err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("tifa: Exposed of the session panicked: %v", p)
			}
		}()
		out := reflect.ValueOf(v).Method(m.Index).Call(nil)[0]
		data, err = json.Marshal(out.Interface())
		if err != nil {
			return nil, false, fmt.Errorf("tifa: encoding the result of Exposed: %w", err)
		}
		return data, string(data) != "null", nil
	}
	return k
}

// BrowserSessionOf returns the session of the request as the browser gets it. It is an error when
// the request holds a session and no session type is registered.
func BrowserSessionOf(c *Ctx) (BrowserSession, error) {
	st := &c.req.sess
	st.mu.Lock()
	have, vtype := st.have, st.vtype
	st.mu.Unlock()
	if !have {
		return BrowserSession{}, nil
	}
	var kind *sessionKind
	if vtype != nil {
		if k, ok := sessionKinds.Load(vtype); ok {
			kind = k.(*sessionKind)
		}
	} else {
		kind = lastKind.Load()
	}
	if kind == nil {
		return BrowserSession{}, errors.New("tifa: a request holds a session, but the process does not know the session type: the generated main registers it, or a call of SetSession or Session does")
	}
	v := kind.load(c)
	if v == nil {
		return BrowserSession{}, nil // the record did not fit S and was dropped
	}
	out := BrowserSession{SignedIn: true}
	if kind.exposed != nil {
		data, ok, err := kind.exposed(v)
		if err != nil {
			return BrowserSession{}, err
		}
		if ok {
			out.Data = data
		}
	}
	return out, nil
}

// SessionChanged reports whether the request called SetSession or ClearSession successfully, so
// that the copy of the session in the browser may be out of date.
func SessionChanged(c *Ctx) bool { return c.req.sess.changed.Load() }
