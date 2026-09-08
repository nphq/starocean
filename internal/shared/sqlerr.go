package shared

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

// SQLite 约束码：CONSTRAINT / PRIMARYKEY / UNIQUE（见 sqlite3.h）。
const (
	sqliteConstraint           = 19
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// IsUniqueViolation 识别 PostgreSQL 23505 / SQLite UNIQUE 冲突（含部分唯一索引）。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	var sqErr *sqlite.Error
	if errors.As(err, &sqErr) {
		switch sqErr.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			return true
		case sqliteConstraint:
			return strings.Contains(strings.ToLower(sqErr.Error()), "unique")
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") ||
		strings.Contains(s, "duplicate key")
}

// LookupPaymentIDByToken 按 client_token 查已提交流水。
// 并发撞唯一索引后胜者可能尚未提交，故短暂重试。
func LookupPaymentIDByToken(ctx context.Context, db *sql.DB, token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, sql.ErrNoRows
	}
	var last = sql.ErrNoRows
	for i := 0; i < 8; i++ {
		var id uuid.UUID
		err := db.QueryRowContext(ctx, `SELECT id FROM payments WHERE client_token = $1`, token).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return uuid.Nil, err
		}
		last = err
		select {
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		case <-time.After(15 * time.Millisecond):
		}
	}
	return uuid.Nil, last
}
