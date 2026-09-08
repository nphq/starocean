package web

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// nullUUID NULL → nil *uuid.UUID 的安全扫描。
func nullUUID(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	id := n.UUID
	return &id
}

func nullTime(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}
