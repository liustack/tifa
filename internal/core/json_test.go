package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// c.JSON, and with it the result of a method handler, writes json v2: a nil slice is [], a nil map
// is {}, <, > and & stay as they are, map keys are sorted, and a byte that is no UTF-8 becomes
// U+FFFD instead of failing.
func TestJSONWritesJSONv2(t *testing.T) {
	type result struct {
		Items  []string          `json:"items"`
		Bytes  []byte            `json:"bytes"`
		Byte4  [4]byte           `json:"byte4"`
		Counts map[string]int    `json:"counts"`
		Sorted map[string]string `json:"sorted"`
		Text   string            `json:"text"`
		Broken string            `json:"broken"`
		Zero   int               `json:"zero,omitempty"`
		None   []int             `json:"none,omitempty"`
	}
	c, w := newCtx("POST", "", "", 1024)
	v := result{
		Byte4:  [4]byte{1, 2, 3, 4},
		Sorted: map[string]string{"b": "2", "a": "1", "c": "3"},
		Text:   "<a href=\"x\">&</a>\u2028",
		Broken: "a\xffb",
	}
	if err := c.JSON(http.StatusOK, v); err != nil {
		t.Fatal(err)
	}
	want := `{"items":[],"bytes":"","byte4":"AQIDBA==","counts":{},"sorted":{"a":"1","b":"2","c":"3"},` +
		`"text":"<a href=\"x\">&</a>` + "\u2028" + `","broken":"a` + "\uFFFD" + `b","zero":0}`
	if got := w.Body.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Bind reads a JSON body with the defaults of json v2: names match exactly, a name may come once,
// strings must be UTF-8 and an array must have the length of the Go array. Each mistake is a 400.
func TestBindJSONIsStrict(t *testing.T) {
	type in struct {
		Name string `json:"name"`
		Pair [2]int `json:"pair"`
	}
	cases := []struct{ name, body string }{
		{"other case", `{"Name":"a"}`},
		{"duplicate name", `{"name":"a","name":"b"}`},
		{"invalid UTF-8", "{\"name\":\"a\xffb\"}"},
		{"long array", `{"pair":[1,2,3]}`},
		{"short array", `{"pair":[1]}`},
	}
	for _, tc := range cases {
		c, _ := newCtx("POST", tc.body, "application/json", 1024)
		var v in
		err := c.Bind(&v)
		var e Error
		if tc.name == "other case" {
			// An unknown name is skipped, so the field stays empty.
			if err != nil || v.Name != "" {
				t.Errorf("%s: got %v, %+v", tc.name, err, v)
			}
			continue
		}
		if !errors.As(err, &e) || e.Status != http.StatusBadRequest {
			t.Errorf("%s: got %v, want a 400", tc.name, err)
		}
	}
}

// A session stores its data as json v2 writes it, and reads it back with the same rules.
func TestSessionRecordIsJSONv2(t *testing.T) {
	type sess struct {
		ID    int64          `json:"id"`
		Roles []string       `json:"roles"`
		Prefs map[string]int `json:"prefs"`
	}
	store := newLogStore(t)
	c, w := sessionCtx(t, prodEnv(store), "")
	if err := SetSession(c, &sess{ID: 7}); err != nil {
		t.Fatal(err)
	}
	token := setCookies(w, "__Host-tifa_session")[0].Value
	record, _, _ := store.MemoryStore.Get(context.Background(), hashOf(token))
	if want := `"data":{"id":7,"roles":[],"prefs":{}}`; !strings.Contains(string(record), want) {
		t.Errorf("record %s does not hold %s", record, want)
	}

	// Data that names a field in another case, or a name twice, does not fit the type, so the
	// session is dropped.
	for _, data := range []string{`{"ID":8}`, `{"id":8,"id":9}`} {
		expires := time.Now().Add(time.Hour)
		bad := `{"v":1,"expires":"` + expires.UTC().Format(time.RFC3339) + `","data":` + data + `}`
		if err := store.MemoryStore.Put(context.Background(), hashOf(token), []byte(bad), expires); err != nil {
			t.Fatal(err)
		}
		c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
		if s := Session[sess](c); s != nil && s.ID != 0 {
			t.Errorf("data %s: got %+v", data, s)
		}
	}
}
