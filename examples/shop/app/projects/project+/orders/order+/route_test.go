package order

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/tifatest"
)

func orderCtx(t *testing.T, method, project, order string) *tifa.Ctx {
	r := httptest.NewRequest(method, "/projects/"+project+"/orders/"+order, nil)
	r.SetPathValue("project", project)
	r.SetPathValue("order", order)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func TestLoadFindsTheOrderInItsProject(t *testing.T) {
	d, err := tifa.Use(orderCtx(t, "GET", "studio", "SH-1041"), Load)
	if err != nil || d.Order.ID != "SH-1041" || len(d.Carriers) != len(orders.Carriers) {
		t.Fatalf("Load: %+v, %v", d, err)
	}
	if _, err := tifa.Use(orderCtx(t, "GET", "weekend", "SH-1041"), Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of another project: %v", err)
	}
	if _, err := tifa.Use(orderCtx(t, "GET", "nowhere", "SH-1041"), Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of an unknown project: %v", err)
	}
}

// The server runs Validate before the handler, and its tifa.Fields reach the form field by field.
func TestInputsAreChecked(t *testing.T) {
	var fields tifa.Fields
	if err := (ShipInput{Carrier: "SF", TrackingNo: "SF1234567890"}).Validate(); err != nil {
		t.Errorf("a good shipment: %v", err)
	}
	if err := (ShipInput{TrackingNo: "SF12"}).Validate(); !errors.As(err, &fields) || len(fields) != 2 {
		t.Errorf("a shipment without a carrier and with a short number: %v", err)
	}
	if err := (RecipientInput{Customer: "林夏", Phone: "13800138000", Address: "上海市徐汇区"}).Validate(); err != nil {
		t.Errorf("a good recipient: %v", err)
	}
	if err := (RecipientInput{Customer: "林夏", Phone: "12345", Address: "上海市徐汇区"}).Validate(); !errors.As(err, &fields) || fields["phone"] == "" {
		t.Errorf("a recipient with a bad phone: %v", err)
	}
}

// Shipping and changing the recipient themselves are tested in the orders package, which resets
// its demo data. Changing an order here would change the data that the next run of this test
// reads, so the handlers are checked on an order already shipped and on orders of another shop.
func TestHandlersRefuseShippedOrdersAndOtherShops(t *testing.T) {
	ship := ShipInput{Carrier: "SF", TrackingNo: "SF1234567890"}
	if _, err := POST(orderCtx(t, "POST", "studio", "SH-1040"), ship); !errors.Is(err, orders.ErrShipped) {
		t.Fatalf("POST of a shipped order: %v", err)
	}
	if _, err := POST(orderCtx(t, "POST", "weekend", "SH-1040"), ship); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("POST of an order of another project: %v", err)
	}
	to := RecipientInput{Customer: "周宁", Phone: "13800138000", Address: "北京市朝阳区"}
	if _, err := PATCH(orderCtx(t, "PATCH", "studio", "SH-1040"), to); !errors.Is(err, orders.ErrRecipientLocked) {
		t.Fatalf("PATCH of a shipped order: %v", err)
	}
	if _, err := PATCH(orderCtx(t, "PATCH", "weekend", "SH-1041"), to); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("PATCH of an order of another project: %v", err)
	}
}

// A note can go on any order, shipped or not, and only on the orders of its own shop.
func TestPUTPutsANoteOnTheOrder(t *testing.T) {
	o, err := PUT(orderCtx(t, "PUT", "studio", "SH-1040"), NoteInput{Text: "  顾客来电，说已收到。 "})
	if err != nil {
		t.Fatal(err)
	}
	last := o.Notes[len(o.Notes)-1]
	if last.Text != "顾客来电，说已收到。" || last.Author != "店主" || last.At.IsZero() {
		t.Errorf("last note %+v", last)
	}
	if _, err := PUT(orderCtx(t, "PUT", "weekend", "SH-1040"), NoteInput{Text: "x"}); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("note on an order of another project: %v", err)
	}
	var fields tifa.Fields
	if err := (NoteInput{Text: " "}).Validate(); !errors.As(err, &fields) || fields["text"] == "" {
		t.Errorf("empty note: %v", err)
	}
}
