package shared

import (
	"errors"
	"testing"
)

func TestIsUniqueViolation(t *testing.T) {
	if !IsUniqueViolation(errors.New("UNIQUE constraint failed: payments.client_token")) {
		t.Fatal("sqlite unique text")
	}
	if !IsUniqueViolation(errors.New("constraint failed: UNIQUE constraint failed: picking_orders.picking_no (2067)")) {
		t.Fatal("sqlite driver text")
	}
	if IsUniqueViolation(errors.New("CHECK constraint failed")) {
		t.Fatal("check should not match")
	}
	if IsUniqueViolation(nil) {
		t.Fatal("nil")
	}
}
