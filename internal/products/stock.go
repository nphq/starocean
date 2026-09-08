package products

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
)

// ErrInsufficientStock 出库超过当前库存。
var ErrInsufficientStock = errors.New("库存不足")

// AdjustStock 调整库存核心流程：行锁 → 校验 → 写库存流水 → 更新库存 → 总账自动过账。
// 由 JSON API 与 HTML 页面共用，保证两条路径行为一致（含 GL 凭证）。
// 调用方负责开启/提交事务（本函数不提交）。
func AdjustStock(ctx context.Context, tx *sql.Tx, id uuid.UUID, adjType string, qty int64, actor string) (int32, error) {
	var beforeStock int32
	var costPrice, productName string
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(current_stock, 0), COALESCE(cost_price,0)::text, COALESCE(name,'') FROM products WHERE id = $1 FOR UPDATE", id).
		Scan(&beforeStock, &costPrice, &productName); err != nil {
		return 0, err
	}
	var afterStock int32
	if adjType == "in" {
		afterStock = beforeStock + int32(qty)
	} else {
		if beforeStock < int32(qty) {
			return 0, fmt.Errorf("%w: 当前库存 %d，出库 %d", ErrInsufficientStock, beforeStock, qty)
		}
		afterStock = beforeStock - int32(qty)
	}

	movementID := uuid.New()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO inventory_movements (id, product_id, type, quantity, reference_type, reference_id, before_stock, after_stock) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
		movementID, id, adjType, int32(qty), "adjustment", nil, beforeStock, afterStock,
	); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE products SET current_stock = $2, updated_at = NOW() WHERE id = $1", id, afterStock); err != nil {
		return 0, err
	}
	if err := ledger.PostStockAdjust(ctx, tx, adjType, int32(qty), costPrice, productName, movementID, actor); err != nil {
		return 0, err
	}
	return afterStock, nil
}
