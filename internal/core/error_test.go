package core

import (
	"strings"
	"testing"
)

func TestFieldsErr(t *testing.T) {
	if err := (Fields{}).Err(); err != nil {
		t.Fatalf("empty Fields: Err() = %v, want nil", err)
	}
	var none Fields
	if err := none.Err(); err != nil {
		t.Fatalf("nil Fields: Err() = %v, want nil", err)
	}
	f := Fields{"carrier": "pick one"}
	err := f.Err()
	got, ok := err.(Fields)
	if !ok || got["carrier"] != "pick one" {
		t.Fatalf("Err() = %#v, want the Fields itself", err)
	}
}

func TestErrorTextsStartWithTifa(t *testing.T) {
	for _, err := range []error{ErrNotFound, ErrSSRBusy} {
		if got := err.Error(); !strings.HasPrefix(got, "tifa: ") {
			t.Errorf("%q does not start with %q", got, "tifa: ")
		}
	}
}
