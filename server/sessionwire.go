package server

import (
	"encoding/json/jsontext"
	"net/http"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// The session reaches the browser in one shape, core.BrowserSession: {"signedIn": bool, "data"?}.
// It comes with a whole page (in the render input or the boot data), in the envelope of a
// navigation answer, in the answer of a call that changed the session, and from /__tifa/session.
// See js/runtime/session.ts for what the browser does with it.

// sessionPath is the path the browser asks for the session, when it holds a copy to check.
const sessionPath = "/__tifa/session"

// SessionOf is the App.Session of an app whose session type is S.
func SessionOf[S any]() { core.RegisterSessionType[S]() }

// enableSessions turns on the session in the pages and answers, and registers the session type.
func (s *server) enableSessions(register func()) {
	if register != nil {
		register()
	}
	s.sess.enabled = true
}

// sessionKey is where the browser keeps its copy of the session: the cookie name and the version
// of the format.
func (s *server) sessionKey() string {
	return s.sessionEnv().Cookie + ":v1"
}

// browserSession returns the session of the request for the browser, nil when the app has none.
func (s *server) browserSession(c *tifa.Ctx) (*core.BrowserSession, error) {
	if !s.sess.enabled {
		return nil, nil
	}
	if a := anonymityOf(c.Request()); a != nil {
		return a.session, a.err
	}
	v, err := core.BrowserSessionOf(c)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// answerSession is browserSession for an answer that is already an error or a redirect: a session
// that cannot be built is logged and left out, since the answer is not to be turned into another.
func (s *server) answerSession(c *tifa.Ctx) *core.BrowserSession {
	v, err := s.browserSession(c)
	if err != nil {
		core.Logger().ErrorContext(c.Context(), "tifa: building the session for the browser failed", "method", c.Request().Method, "path", c.Request().URL.Path, "err", err)
		return nil
	}
	return v
}

// callSession is the session an answer to a call carries: only when the call changed it.
func (s *server) callSession(c *tifa.Ctx) *core.BrowserSession {
	if !core.SessionChanged(c) {
		return nil
	}
	return s.answerSession(c)
}

// pageSession is what a whole page carries of the session.
type pageSession struct {
	json   jsontext.Value // the session of the request, nil when the app has none
	key    string
	head   string // the script and the style at the start of the head
	signed bool
}

func (s *server) pageSession(c *tifa.Ctx) (pageSession, error) {
	if anonymityOf(c.Request()) != nil && s.sess.enabled {
		// A page every visitor gets carries no session. The head script reads the copy the
		// browser keeps, and the browser asks the server once it has started.
		key := s.sessionKey()
		return pageSession{key: key, head: sessionHead(key)}, nil
	}
	v, err := s.browserSession(c)
	if err != nil || v == nil {
		return pageSession{}, err
	}
	data, err := core.MarshalJSON(v)
	if err != nil {
		return pageSession{}, err
	}
	key := s.sessionKey()
	return pageSession{json: data, key: key, head: sessionHead(key), signed: v.SignedIn}, nil
}

// sessionHead is the HTML at the start of the head of every page of an app with sessions: a script
// that, when the server did not write data-session on <html> (a page the server answers for every
// visitor alike), sets it to "in" if the copy of the session in localStorage says someone is
// signed in, and the style that hides what is marked for the other state. The browser keeps the
// copy under key. The script is the same on every page, so one hash of it serves a CSP.
func sessionHead(key string) string {
	// htmlJSON escapes <, > and & so the key cannot end the script element.
	k, _ := htmlJSON(key)
	var b strings.Builder
	b.WriteString(`<script>(function(){var h=document.documentElement;if(h.hasAttribute("data-session"))return;try{var v=JSON.parse(localStorage.getItem(`)
	b.Write(k)
	b.WriteString(`));if(v&&typeof v==="object"&&v.signedIn===true)h.setAttribute("data-session","in")}catch(e){}})()</script>`)
	b.WriteString(sessionStyle)
	return b.String()
}

// sessionStyle hides what is marked data-session="in" unless someone is signed in, and what is
// marked data-session="out" when someone is.
const sessionStyle = `<style>html:not([data-session="in"]) [data-session="in"],html[data-session="in"] [data-session="out"]{display:none}</style>`

// htmlTag is the opening tag of the document of a page that skips SSR. A page that carries the
// session says in or out, so that the head script reads the copy only on a page that carries none.
func (p pageSession) htmlTag() string {
	switch {
	case p.signed:
		return `<html data-session="in">`
	case p.json != nil:
		return `<html data-session="out">`
	}
	return "<html>"
}

// sealSessionAnswer makes the answer of an error flow that carries the session unstorable: a
// header that would let a shared cache keep it is replaced, and logged.
func (s *server) sealSessionAnswer(c *tifa.Ctx) {
	if err := checkSessionCache(c.Writer().Header()); err != nil {
		core.Logger().ErrorContext(c.Context(), "tifa: an answer with the session was made no-store", "method", c.Request().Method, "path", c.Request().URL.Path, "err", err)
		c.Writer().Header().Set("Cache-Control", "no-store")
	}
}

// sessionHandler answers GET and HEAD of /__tifa/session with the session of the request. It runs
// inside OnRequest, so a middleware that signs visitors in must let it through.
func (s *server) sessionHandler(c *tifa.Ctx) error {
	r := c.Request()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		c.SetHeader("Allow", "GET, HEAD")
		return tifa.Error{Status: http.StatusMethodNotAllowed, Message: http.StatusText(http.StatusMethodNotAllowed)}
	}
	if build := c.Header(buildHeader); build != "" && s.pagesServed() && build != s.front.Load().build.ID {
		return s.buildChanged(c)
	}
	v, err := s.browserSession(c)
	if err != nil {
		return err
	}
	c.Writer().Header().Del("ETag")
	c.Writer().Header().Del("Last-Modified")
	c.SetHeader("Cache-Control", "private, no-store")
	return c.JSON(http.StatusOK, v)
}
