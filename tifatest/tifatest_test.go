package tifatest_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

type order struct {
	Project string `json:"project"`
	ID      string `json:"id"`
	Tab     string `json:"tab"`
}

func layout(c *tifa.Ctx) (string, error) {
	return "project " + c.Param("project"), nil
}

func load(c *tifa.Ctx) (order, error) {
	if _, err := tifa.Use(c, layout); err != nil {
		return order{}, err
	}
	return order{Project: c.Param("project"), ID: c.Param("order"), Tab: c.Query("tab")}, nil
}

func TestCtxReadsParamsAndQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1?tab=items", nil)
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1")
	c, _ := tifatest.NewCtx(t, r)
	got, err := load(c)
	if err != nil || got != (order{"studio", "SH-1", "items"}) {
		t.Fatalf("load: %+v, %v", got, err)
	}
}

func TestUseRunsALoadOnce(t *testing.T) {
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	calls := 0
	counted := func(*tifa.Ctx) (int, error) { calls++; return calls, nil }
	for range 2 {
		if n, err := tifa.Use(c, counted); n != 1 || err != nil {
			t.Fatalf("Use: %d, %v", n, err)
		}
	}
}

func TestUseKeepsTheRulesOfLoad(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	_, err := tifa.Use(c, func(c *tifa.Ctx) (int, error) { return 0, c.JSON(200, 1) })
	if err == nil || w.Body.Len() != 0 {
		t.Fatalf("a Load run through Use wrote %q, err %v", w.Body.String(), err)
	}
}

func TestRecorderHoldsWhatAHandlerWrites(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("POST", "/orders", strings.NewReader(`{"id":"SH-2"}`)))
	c.Request().Header.Set("Content-Type", "application/json")
	var in order
	if err := c.Bind(&in); err != nil || in.ID != "SH-2" {
		t.Fatalf("Bind: %+v, %v", in, err)
	}
	if err := c.JSON(201, in); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"id":"SH-2"`) {
		t.Fatalf("recorded %d %s", w.Code, w.Body.String())
	}
}

func TestBodyLimitIsTheDefault(t *testing.T) {
	body := `{"id":"` + strings.Repeat("x", 1<<20) + `"}`
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("POST", "/orders", strings.NewReader(body)))
	c.Request().Header.Set("Content-Type", "application/json")
	var e tifa.Error
	if err := c.Bind(&order{}); !errors.As(err, &e) || e.Status != 413 {
		t.Fatalf("a body over 1 MiB: %v", err)
	}
}

func onRequest(c *tifa.Ctx) error {
	if c.Header("Authorization") == "" {
		return tifa.Error{Status: 401, Message: "sign in"}
	}
	return c.Next()
}

func TestNextRunsTheRouteOfOnRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	c, _ := tifatest.NewCtx(t, r)
	ran := false
	tifatest.SetNext(c, func() error { ran = true; return nil })
	var e tifa.Error
	if err := onRequest(c); !errors.As(err, &e) || e.Status != 401 || ran {
		t.Fatalf("without a token: %v, route ran %v", err, ran)
	}
	r.Header.Set("Authorization", "Bearer x")
	c, _ = tifatest.NewCtx(t, r)
	tifatest.SetNext(c, func() error { ran = true; return nil })
	if err := onRequest(c); err != nil || !ran {
		t.Fatalf("with a token: %v, route ran %v", err, ran)
	}
}

func TestNextOutsideOnRequestFails(t *testing.T) {
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	if err := c.Next(); err == nil {
		t.Fatal("Next without tifatest.Next must fail as it does in a handler")
	}
}

func TestCleanupRemovesMultipartFiles(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("hello"))
	form.Close()
	var names []string
	t.Run("request", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/upload", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		c, _ := tifatest.NewCtx(t, r)
		// With maxMemory 0 the file goes to disk. No http.Server runs to remove it after the request.
		if err := c.Request().ParseMultipartForm(0); err != nil {
			t.Fatal(err)
		}
		names = append(names, tempFile(t, c))
	})
	for _, name := range names {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is left after the test: %v", name, err)
		}
	}
}

func tempFile(t *testing.T, c *tifa.Ctx) string {
	f, err := c.Request().MultipartForm.File["file"][0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, ok := f.(*os.File)
	if !ok {
		t.Fatalf("the file part is held in memory as %T", f)
	}
	return file.Name()
}
