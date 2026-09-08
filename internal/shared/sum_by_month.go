package shared

import (
	"context"
	"database/sql"

	"github.com/shopspring/decimal"
)

// SumByMonthSQLite 执行按 YYYY-MM 分组的 SUM 查询（SQLite 专用，GROUP BY 一次性聚合），
// 缺失月份在调用方补 0。用于现金流趋势/月度图表等按月汇总场景。
func SumByMonthSQLite(ctx context.Context, db *sql.DB, query string, args ...any) (map[string]decimal.Decimal, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]decimal.Decimal{}
	for rows.Next() {
		var label string
		var sum decimal.Decimal
		if err := rows.Scan(&label, &sum); err != nil {
			return nil, err
		}
		result[label] = sum
	}
	return result, rows.Err()
}
