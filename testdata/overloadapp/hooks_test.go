package overloadapp

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

func OnRequest(c *tifa.Ctx) error {
	err := c.Next()
	if errors.Is(err, tifa.ErrSSRBusy) {
		c.SetHeader("Retry-After", "1")
		return tifa.Error{Status: 503, Message: "Please try again shortly"}
	}
	return err
}

func TestOnRequestHandlesOnlySSRBusy(t *testing.T) {
	other := errors.New("another render error")
	for _, err := range []error{fmt.Errorf("render: %w", tifa.ErrSSRBusy), other, nil} {
		c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
		tifatest.SetNext(c, func() error { return err })
		got := OnRequest(c)
		if errors.Is(err, tifa.ErrSSRBusy) {
			var public tifa.Error
			if !errors.As(got, &public) || public.Status != 503 || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("overload: %v, headers %v", got, w.Header())
			}
		} else if got != err || w.Header().Get("Retry-After") != "" {
			t.Fatalf("changed another result: %v, headers %v", got, w.Header())
		}
	}
}
