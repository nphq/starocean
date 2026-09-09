package shared

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Turso/SQLite 约束冲突形如：
//   turso: constraint failed: UNIQUE constraint failed: t.a (19)
// 统一按文本识别（大小写不敏感），不依赖驱动特有错误类型。
// IsUniqueViolation 识别 UNIQUE 冲突（含部分唯一索引）。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
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
