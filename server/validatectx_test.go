package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// noteInput checks itself with the Ctx: the message is in the language of the request.
type noteInput struct {
	Text string `json:"text"`
}

var emptyNote = map[string]string{"zh-CN": "请输入内容", "en-US": "Enter some text"}

func (in noteInput) Validate(c *tifa.Ctx) error {
	if in.Text == "" {
		return tifa.Fields{"text": emptyNote[c.Language()] + " " + c.Param("id") + " " + c.Query("q")}
	}
	return nil
}

func validateCtxApp() App {
	post := Input(func(c *tifa.Ctx, in noteInput) error { return nil }, BindBody)
	app := i18nTestApp(false)
	app.Routes = append(app.Routes,
		Route{ID: "app/store/notes/note+/route", Path: "/store/notes/{id}", POST: post, Languages: storeLanguages},
		Route{ID: "app/plain/route", Path: "/plain", POST: post},
	)
	return app
}

// Validate may take the Ctx. It sees the language of the request, taken from the URL, and the
// rest of what a handler reads.
func TestValidateGetsTheCtx(t *testing.T) {
	app := validateCtxApp()
	s := newAppServer(t, app)
	s.setI18n(app.I18n)
	cases := []struct{ target, want string }{
		{"/store/notes/7?q=a", "请输入内容 7 a"},
		{"/en-US/store/notes/8?q=b", "Enter some text 8 b"},
		{"/zh-CN/store/notes/9", ""},
	}
	for _, tc := range cases {
		w := send(s, "POST", tc.target, `{"text":""}`)
		if tc.want == "" {
			// The default language with its prefix is a redirect, as for any route.
			if w.Code != 308 {
				t.Errorf("POST %s: %d, want 308", tc.target, w.Code)
			}
			continue
		}
		if want := `"fields":{"text":"` + tc.want + `"}`; w.Code != 422 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("POST %s: %d %s, want 422 with %s", tc.target, w.Code, w.Body.String(), want)
		}
	}
	if w := send(s, "POST", "/en-US/store/notes/8", `{"text":"hi"}`); w.Code != 204 {
		t.Errorf("a valid input: %d %s, want 204", w.Code, w.Body.String())
	}
}

// A route outside every area, and a site without languages, give the language they have.
func TestValidateLanguageOutsideAreas(t *testing.T) {
	app := validateCtxApp()
	s := newAppServer(t, app)
	s.setI18n(app.I18n)
	if w := send(s, "POST", "/plain", `{"text":""}`); w.Code != 422 || !strings.Contains(w.Body.String(), "请输入内容") {
		t.Errorf("outside every area: %d %s, want the default language", w.Code, w.Body.String())
	}
	plain := newAppServer(t, App{Routes: []Route{{ID: "app/plain/route", Path: "/plain", POST: Input(func(c *tifa.Ctx, in noteInput) error { return nil }, BindBody)}}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/plain", strings.NewReader(`{"text":""}`))
	r.Header.Set("Content-Type", "application/json")
	plain.ServeHTTP(w, r)
	if want := `"fields":{"text":"  "}`; w.Code != 422 || !strings.Contains(w.Body.String(), want) {
		t.Errorf("without languages: %d %s, want %s", w.Code, w.Body.String(), want)
	}
}
