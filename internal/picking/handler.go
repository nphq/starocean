package picking

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

type generateInput struct {
	Type string `json:"type"`
}

type pickingItemUpdateInput struct {
	PickedQuantity string `json:"picked_quantity"`
}

func (h *Handler) PickingOrdersPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	status := c.Query("status")
	query := `SELECT id, picking_no, type, status, order_date, COALESCE(assigned_to,''), COALESCE(notes,''),
	                 created_at, updated_at, COALESCE(company_id,'default')
	          FROM picking_orders`
	countQuery := `SELECT COUNT(*) FROM picking_orders`
	args := []interface{}{}
	if status != "" {
		query += ` WHERE status = $1 ORDER BY order_date DESC, created_at DESC LIMIT $2 OFFSET $3`
		countQuery += ` WHERE status = $1`
		args = append(args, status, 20, (page-1)*20)
	} else {
		query += ` ORDER BY order_date DESC, created_at DESC LIMIT $1 OFFSET $2`
		args = append(args, 20, (page-1)*20)
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var orders []models.PickingOrder
	for rows.Next() {
		var o models.PickingOrder
		if err := rows.Scan(&o.ID, &o.PickingNo, &o.Type, &o.Status, &o.OrderDate, &o.AssignedTo, &o.Notes, &o.CreatedAt, &o.UpdatedAt, &o.CompanyID); err != nil {
			continue
		}
		orders = append(orders, o)
	}
	var count int64
	var countErr error
	if status != "" {
		countErr = h.db.QueryRowContext(ctx, countQuery, status).Scan(&count)
	} else {
		countErr = h.db.QueryRowContext(ctx, countQuery).Scan(&count)
	}
	if countErr != nil {
		shared.JSONInternal(c, countErr)
		return
	}
	shared.JSONOK(c, shared.PageResult[models.PickingOrder]{Items: shared.EmptySlice(orders), Total: count, Page: page})
}

func (h *Handler) GeneratePickingOrder(c *gin.Context) {
	ctx := c.Request.Context()
	var in generateInput
	if !shared.BindJSON(c, &in) {
		return
	}
	pickingType := in.Type
	if pickingType == "" {
		pickingType = "by_product"
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM picking_orders WHERE order_date = CURRENT_DATE`).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	pickingNo := fmt.Sprintf("PK-%s-%03d", time.Now().Format("20060102"), count+1)

	var pickingID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`INSERT INTO picking_orders (id, picking_no, type, company_id) VALUES ($1, $2, $3, 'default') RETURNING id`,
		uuid.New(), pickingNo, pickingType).Scan(&pickingID)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT si.product_id, p.name, p.code, SUM(si.quantity) as total_qty,
		        so.customer_id, c.name
		 FROM sales_order_items si
		 JOIN sales_orders so ON si.order_id = so.id
		 JOIN products p ON si.product_id = p.id
		 LEFT JOIN customers c ON so.customer_id = c.id
		 WHERE so.status = 'confirmed'
		   AND NOT EXISTS (
		     SELECT 1 FROM picking_items pi
		     JOIN picking_orders po ON pi.picking_id = po.id
		     WHERE pi.source_order_id = so.id AND pi.product_id = si.product_id
		       AND po.status IN ('pending', 'in_progress')
		   )
		 GROUP BY si.product_id, p.name, p.code, so.customer_id, c.name
		 ORDER BY p.name`)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	inserted := 0
	for rows.Next() {
		var productID uuid.UUID
		var productName, productCode string
		var totalQty int32
		var customerID uuid.UUID
		var customerName string
		if err := rows.Scan(&productID, &productName, &productCode, &totalQty, &customerID, &customerName); err != nil {
			continue
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO picking_items (id, picking_id, product_id, product_name, product_code, required_quantity, source_customer_id, source_customer_name)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			uuid.New(), pickingID, productID, productName, productCode, decimal.NewFromInt(int64(totalQty)), customerID, customerName)
		if err == nil {
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONCreated(c, gin.H{"ok": true, "id": pickingID, "inserted": inserted})
}

func (h *Handler) PickingDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid id")
		return
	}
	var o models.PickingOrder
	err = h.db.QueryRowContext(ctx,
		`SELECT id, picking_no, type, status, order_date, COALESCE(assigned_to,''), COALESCE(notes,''),
		        created_at, updated_at, COALESCE(company_id,'default')
		 FROM picking_orders WHERE id = $1`, id).Scan(
		&o.ID, &o.PickingNo, &o.Type, &o.Status, &o.OrderDate, &o.AssignedTo, &o.Notes,
		&o.CreatedAt, &o.UpdatedAt, &o.CompanyID)
	if err != nil {
		shared.JSONNotFound(c, "not found")
		return
	}
	itemRows, err := h.db.QueryContext(ctx,
		`SELECT id, picking_id, product_id, COALESCE(product_name,''), COALESCE(product_code,''),
		        required_quantity, picked_quantity, COALESCE(source_order_id, '00000000-0000-0000-0000-000000000000'),
		        COALESCE(source_customer_id, '00000000-0000-0000-0000-000000000000'), COALESCE(source_customer_name,''),
		        COALESCE(status,''), created_at
		 FROM picking_items WHERE picking_id = $1 ORDER BY product_name`, id)
	if err != nil {
		shared.JSONOK(c, gin.H{"order": o, "items": []models.PickingItem{}})
		return
	}
	defer itemRows.Close()
	var items []models.PickingItem
	for itemRows.Next() {
		var it models.PickingItem
		if err := itemRows.Scan(&it.ID, &it.PickingID, &it.ProductID, &it.ProductName, &it.ProductCode,
			&it.RequiredQuantity, &it.PickedQuantity, &it.SourceOrderID, &it.SourceCustomerID, &it.SourceCustomerName,
			&it.Status, &it.CreatedAt); err != nil {
			continue
		}
		items = append(items, it)
	}
	shared.JSONOK(c, gin.H{"order": o, "items": shared.EmptySlice(items)})
}

func (h *Handler) PickingItemUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	pickingID, _ := uuid.Parse(c.Param("id"))
	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid item id")
		return
	}
	var in pickingItemUpdateInput
	if !shared.BindJSON(c, &in) {
		return
	}
	pickedQty := in.PickedQuantity
	_, err = h.db.ExecContext(ctx,
		`UPDATE picking_items SET picked_quantity = $1, status = CASE WHEN $1::decimal >= required_quantity THEN 'picked' ELSE 'shortage' END
		 WHERE id = $2 AND picking_id = $3`, pickedQty, itemID, pickingID)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PickingComplete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid id")
		return
	}
	_, err = h.db.ExecContext(ctx,
		`UPDATE picking_orders SET status = 'completed', updated_at = NOW() WHERE id = $1 AND status IN ('pending', 'in_progress')`, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}
