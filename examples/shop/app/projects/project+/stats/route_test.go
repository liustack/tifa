package stats

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

func statsCtx(t *testing.T, project string) *tifa.Ctx {
	r := httptest.NewRequest("GET", "/projects/"+project+"/stats", nil)
	r.SetPathValue("project", project)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func TestLoadReadsTheSalesOfTheProject(t *testing.T) {
	s, err := tifa.Use(statsCtx(t, "weekend"), Load)
	if err != nil || len(s.Days) != 30 || len(s.TopProducts) == 0 || s.TopProducts[0].Name != "野餐垫" {
		t.Fatalf("Load: %+v, %v", s, err)
	}
	if _, err := tifa.Use(statsCtx(t, "nowhere"), Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("stats of an unknown project: %v", err)
	}
}
