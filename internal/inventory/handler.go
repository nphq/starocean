package inventory

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) InventoryPage(c *gin.Context) {
	page := shared.GetPage(c)
	ctx := c.Request.Context()

	rows, err := h.db.QueryContext(ctx, `SELECT im.id, im.product_id, COALESCE(p.name,''), COALESCE(p.code,''),
		im.type, im.quantity, COALESCE(im.reference_type,''),
		im.before_stock, im.after_stock, COALESCE(im.created_at,'1970-01-01'),
		COALESCE(im.company_id,'default')
		FROM inventory_movements im LEFT JOIN products p ON im.product_id = p.id
		ORDER BY im.created_at DESC LIMIT $1 OFFSET $2`, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var movements []models.InventoryMovement
	for rows.Next() {
		var m models.InventoryMovement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductName, &m.ProductCode,
			&m.Type, &m.Quantity, &m.ReferenceType,
			&m.BeforeStock, &m.AfterStock, &m.CreatedAt, &m.CompanyID); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		movements = append(movements, m)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_movements").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.InventoryMovement]{Items: shared.EmptySlice(movements), Total: count, Page: page})
}

func (h *Handler) InventoryByProductPage(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("productId"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	page := shared.GetPage(c)
	ctx := c.Request.Context()

	rows, err := h.db.QueryContext(ctx, `SELECT im.id, im.product_id, COALESCE(p.name,''), COALESCE(p.code,''),
		im.type, im.quantity, COALESCE(im.reference_type,''),
		im.before_stock, im.after_stock, COALESCE(im.created_at,'1970-01-01'),
		COALESCE(im.company_id,'default')
		FROM inventory_movements im LEFT JOIN products p ON im.product_id = p.id
		WHERE im.product_id = $1
		ORDER BY im.created_at DESC LIMIT $2 OFFSET $3`, productID, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var movements []models.InventoryMovement
	for rows.Next() {
		var m models.InventoryMovement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductName, &m.ProductCode,
			&m.Type, &m.Quantity, &m.ReferenceType,
			&m.BeforeStock, &m.AfterStock, &m.CreatedAt, &m.CompanyID); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		movements = append(movements, m)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_movements WHERE product_id = $1", productID).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.InventoryMovement]{Items: shared.EmptySlice(movements), Total: count, Page: page})
}
