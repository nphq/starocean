package shared

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	pg := &pgconn.PgError{Code: "23505"}
	if !IsUniqueViolation(pg) {
		t.Fatal("pg 23505")
	}
	if !IsUniqueViolation(fmt.Errorf("wrap: %w", pg)) {
		t.Fatal("wrapped pg 23505")
	}
	if IsUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("fk should not match")
	}
	if !IsUniqueViolation(errors.New("UNIQUE constraint failed: payments.client_token")) {
		t.Fatal("sqlite unique text")
	}
	if !IsUniqueViolation(errors.New("duplicate key value violates unique constraint \"idx_payments_client_token\"")) {
		t.Fatal("pg unique text")
	}
	if IsUniqueViolation(errors.New("CHECK constraint failed")) {
		t.Fatal("check should not match")
	}
	if IsUniqueViolation(nil) {
		t.Fatal("nil")
	}
}
