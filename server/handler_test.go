package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

type shipInput struct {
	Carrier string `json:"carrier"`
}

func (in *shipInput) Validate() error {
	switch in.Carrier {
	case "":
		return errors.New("carrier is required")
	case "drone":
		return tifa.Fields{"carrier": "no drones"}
	case "teleport":
		return tifa.Error{Status: 409, Message: "not yet"}
	}
	return nil
}

type listInput struct {
	Status string `json:"status"`
}

type shipment struct {
	Carrier string `json:"carrier"`
	Order   string `json:"order"`
}

func typedApp(shipped *[]string) App {
	return App{Routes: []Route{{
		ID: "app/orders/order+/route", Path: "/orders/{order}", SSR: true,
		GET: InputOutput(func(c *tifa.Ctx, in listInput) ([]string, error) {
			return []string{c.Param("order"), in.Status}, nil
		}, (*tifa.Ctx).BindQuery),
		POST: Input(func(c *tifa.Ctx, in shipInput) error {
			*shipped = append(*shipped, in.Carrier)
			return nil
		}, (*tifa.Ctx).Bind),
		PUT: Output(func(c *tifa.Ctx) (shipment, error) {
			return shipment{Carrier: "SF", Order: c.Param("order")}, nil
		}),
		PATCH: InputOutput(func(c *tifa.Ctx, in shipInput) (*shipment, error) {
			if in.Carrier == "lost" {
				return nil, tifa.ErrNotFound
			}
			return &shipment{Carrier: in.Carrier, Order: c.Param("order")}, nil
		}, (*tifa.Ctx).Bind),
	}}}
}

func TestTypedHandlers(t *testing.T) {
	var shipped []string
	s := newAppServer(t, typedApp(&shipped))
	cases := []struct {
		method, target, body string
		status               int
		want                 string
	}{
		{"GET", "/orders/7?status=open", "", 200, `["7","open"]`},
		{"POST", "/orders/7", `{"carrier":"SF"}`, 204, ""},
		{"POST", "/orders/7", `{"carrier":""}`, 400, `{"error":"carrier is required"}`},
		{"POST", "/orders/7", `{"carrier":"drone"}`, 422, `{"error":"Unprocessable Entity","fields":{"carrier":"no drones"}}`},
		{"POST", "/orders/7", `{"carrier":"teleport"}`, 409, `{"error":"not yet"}`},
		{"POST", "/orders/7", `{"carrier":`, 400, `{"error":"invalid JSON body"}`},
		{"PUT", "/orders/7", "", 200, `{"carrier":"SF","order":"7"}`},
		{"PATCH", "/orders/7", `{"carrier":"JD"}`, 200, `{"carrier":"JD","order":"7"}`},
		{"PATCH", "/orders/7", `{"carrier":"lost"}`, 404, `{"error":"Not Found"}`},
	}
	for _, tc := range cases {
		w := send(s, tc.method, tc.target, tc.body)
		if w.Code != tc.status || strings.TrimSpace(w.Body.String()) != tc.want {
			t.Errorf("%s %s %s: %d %s, want %d %s", tc.method, tc.target, tc.body, w.Code, w.Body.String(), tc.status, tc.want)
		}
	}
	if len(shipped) != 1 || shipped[0] != "SF" {
		t.Errorf("the handler ran for %v, want only the valid input", shipped)
	}
}
