package orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/shopspring/decimal"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

type orderItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int32     `json:"quantity"`
	UnitPrice string    `json:"unit_price"`
	TaxRate   string    `json:"tax_rate"`
}

type salesCreateRequest struct {
	CustomerID uuid.UUID              `json:"customer_id"`
	OrderDate  string                 `json:"order_date"`
	Notes      string                 `json:"notes"`
	Properties map[string]interface{} `json:"properties"`
	Items      []orderItemInput       `json:"items"`
}

type purchaseCreateRequest struct {
	SupplierID uuid.UUID              `json:"supplier_id"`
	OrderDate  string                 `json:"order_date"`
	Notes      string                 `json:"notes"`
	Properties map[string]interface{} `json:"properties"`
	Items      []orderItemInput       `json:"items"`
}

func parseOrderDate(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Now()
		}
	}
	return t
}

// validateOrderItems P0: 写入前校验明细，避免非法金额静默归零、数量非法或空单据入库。
func validateOrderItems(items []orderItemInput) error {
	if len(items) == 0 {
		return errors.New("订单明细不能为空")
	}
	if len(items) > 200 {
		return errors.New("订单明细过多（最多200行）")
	}
	for i, it := range items {
		if it.ProductID == uuid.Nil {
			return fmt.Errorf("第%d行: 商品不能为空", i+1)
		}
		if it.Quantity <= 0 || it.Quantity > 1000000 {
			return fmt.Errorf("第%d行: 数量必须在1~1000000之间", i+1)
		}
		if _, err := shared.ParsePrice(it.UnitPrice); err != nil {
			return fmt.Errorf("第%d行: 单价格式无效", i+1)
		}
	}
	return nil
}

// isBusinessError P0: 用户可纠正的业务错误（库存/信用/状态流转/不存在）返回400，
// 仅未知系统错误返回500，避免把业务提示吞成“内部服务器错误”。
func isBusinessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, k := range []string{
		"库存不足", "库存扣减数量", "库存增加数量", "超出客户信用额度",
		"cannot transition", "cannot cancel", "customer not found",
		"订单不存在", "数量必须", "单价格式", "明细不能为空", "明细过多",
		"仅草稿", "只能作废", "不能删除", "存在关联单据",
	} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

func writeOrderError(c *gin.Context, err error) {
	if isBusinessError(err) {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	shared.JSONInternal(c, err)
}

func (h *Handler) SalesPage(c *gin.Context) {
	page := shared.GetPage(c)
	ctx := c.Request.Context()
	limit := int32(20)
	offset := int32((page - 1) * 20)

	rows, err := h.db.QueryContext(ctx, `
		SELECT so.id, so.order_no, COALESCE(so.customer_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(c.name, '') as customer_name, COALESCE(so.status, 'draft'),
		       COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
		       COALESCE(so.order_date, '1970-01-01'), COALESCE(so.notes, ''),
		       COALESCE(so.created_at, '1970-01-01'),
		       COALESCE(so.company_id, 'default'), COALESCE(so.properties, '{}')
		FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
		ORDER BY so.created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var orders []models.SalesOrder
	for rows.Next() {
		var o models.SalesOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
			&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
			&o.CompanyID, &o.Properties); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	var count int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sales_orders").Scan(&count)
	shared.JSONOK(c, shared.PageResult[models.SalesOrder]{Items: shared.EmptySlice(orders), Total: count, Page: page})
}

func (h *Handler) SalesCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var req salesCreateRequest
	if !shared.BindJSON(c, &req) {
		return
	}
	if req.CustomerID == uuid.Nil {
		shared.JSONBadRequest(c, "客户不能为空")
		return
	}
	if err := validateOrderItems(req.Items); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	items := make([]SalesOrderItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, SalesOrderItemInput(it))
	}

	payload := map[string]interface{}{
		"customer_id": req.CustomerID.String(),
		"notes":       req.Notes,
		"properties":  req.Properties,
	}
	res := shared.FireBefore(ctx, "sales_order.creating", payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	if notes, ok := shared.PatchString(res, "notes"); ok {
		req.Notes = notes
	}
	props := shared.PropertiesFromPatch(res, req.Properties)

	in := SalesOrderInput{
		CustomerID: req.CustomerID,
		OrderDate:  parseOrderDate(req.OrderDate),
		Notes:      req.Notes,
		Properties: props,
		Items:      items,
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	orderID, _, err := CreateSalesOrder(ctx, tx, in)
	if err != nil {
		writeOrderError(c, err)
		return
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("sales_order.created", map[string]interface{}{"order_id": orderID.String()})
	shared.JSONCreated(c, gin.H{"ok": true, "id": orderID})
}

func (h *Handler) SalesDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}

	order, err := getSalesOrder(ctx, h.db, id)
	if err != nil {
		shared.JSONNotFound(c, "订单不存在")
		return
	}

	itemRows, err := h.db.QueryContext(ctx, `
		SELECT soi.id, COALESCE(soi.order_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(soi.product_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       soi.quantity, COALESCE(soi.unit_price, 0), COALESCE(soi.amount, 0), COALESCE(soi.tax_rate, 0)
		FROM sales_order_items soi
		LEFT JOIN products p ON soi.product_id = p.id
		WHERE soi.order_id = $1`, id)
	if err != nil {
		shared.JSONNotFound(c, "订单不存在")
		return
	}
	defer itemRows.Close()

	var items []models.SalesOrderItem
	for itemRows.Next() {
		var it models.SalesOrderItem
		if err := itemRows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount, &it.TaxRate); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		items = append(items, it)
	}

	netAmt, taxAmt := orderLineTaxSum(items)
	shared.JSONOK(c, gin.H{"order": order, "items": shared.EmptySlice(items), "net_amount": netAmt, "tax_amount": taxAmt})
}

func (h *Handler) SalesConfirm(c *gin.Context) {
	h.runSalesAction(c, ConfirmSalesOrder, "sales_order.confirming", "sales_order.confirmed")
}

func (h *Handler) SalesShip(c *gin.Context) {
	h.runSalesAction(c, ShipSalesOrder, "sales_order.shipping", "sales_order.shipped")
}

func (h *Handler) SalesInvoice(c *gin.Context) {
	h.runSalesAction(c, InvoiceSalesOrder, "sales_order.invoicing", "sales_order.invoiced")
}

func (h *Handler) SalesCancel(c *gin.Context) {
	h.runSalesAction(c, CancelSalesOrder, "sales_order.cancelling", "sales_order.cancelled")
}

func (h *Handler) runSalesAction(c *gin.Context, fn func(context.Context, *sql.Tx, uuid.UUID) error, before, after string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	payload := map[string]interface{}{"order_id": id.String()}
	res := shared.FireBefore(ctx, before, payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	if err := fn(ctx, tx, id); err != nil {
		writeOrderError(c, err)
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, after, id, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, "财务自动过账失败，本次操作已回滚")
		return
	}
	if props := shared.PropertiesFromPatch(res, nil); len(props) > 0 {
		if err := shared.MergeProperties(ctx, tx, "sales_orders", id, props); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter(after, payload)
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) SalesDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	payload := map[string]interface{}{"order_id": id.String()}
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "sales_order.deleting", payload)) {
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	if err := DeleteSalesOrder(ctx, tx, id); err != nil {
		writeOrderError(c, err)
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, "sales_order.deleted", id, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("sales_order.deleted", payload)
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) SalesSearchAPI(c *gin.Context) {
	ctx := c.Request.Context()
	query := c.Query("q")

	rows, err := h.db.QueryContext(ctx, `
		SELECT so.id, so.order_no, COALESCE(so.customer_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(c.name, '') as customer_name, COALESCE(so.status, 'draft'),
		       COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
		       COALESCE(so.order_date, '1970-01-01'), COALESCE(so.notes, ''),
		       COALESCE(so.created_at, '1970-01-01'),
		       COALESCE(so.company_id, 'default'), COALESCE(so.properties, '{}')
		FROM sales_orders so
		LEFT JOIN customers c ON so.customer_id = c.id
		WHERE so.order_no LIKE '%' || $1 || '%'
		   OR c.name LIKE '%' || $1 || '%'
		ORDER BY so.created_at DESC
		LIMIT 20`, query)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var orders []models.SalesOrder
	for rows.Next() {
		var o models.SalesOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
			&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
			&o.CompanyID, &o.Properties); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		orders = append(orders, o)
	}
	shared.JSONOK(c, shared.EmptySlice(orders))
}

func (h *Handler) PurchasesPage(c *gin.Context) {
	page := shared.GetPage(c)
	ctx := c.Request.Context()
	limit := int32(20)
	offset := int32((page - 1) * 20)

	rows, err := h.db.QueryContext(ctx, `
		SELECT po.id, po.order_no, COALESCE(po.supplier_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(s.name, '') as supplier_name, COALESCE(po.status, 'draft'),
		       COALESCE(po.total_amount, 0), COALESCE(po.paid_amount, 0),
		       COALESCE(po.order_date, '1970-01-01'), COALESCE(po.notes, ''),
		       COALESCE(po.created_at, '1970-01-01'),
		       COALESCE(po.company_id, 'default'), COALESCE(po.properties, '{}')
		FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id
		ORDER BY po.created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var orders []models.PurchaseOrder
	for rows.Next() {
		var o models.PurchaseOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName,
			&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
			&o.CompanyID, &o.Properties); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		orders = append(orders, o)
	}

	var count int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM purchase_orders").Scan(&count)
	shared.JSONOK(c, shared.PageResult[models.PurchaseOrder]{Items: shared.EmptySlice(orders), Total: count, Page: page})
}

func (h *Handler) PurchaseCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var req purchaseCreateRequest
	if !shared.BindJSON(c, &req) {
		return
	}
	if req.SupplierID == uuid.Nil {
		shared.JSONBadRequest(c, "供应商不能为空")
		return
	}
	if err := validateOrderItems(req.Items); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	items := make([]PurchaseOrderItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, PurchaseOrderItemInput(it))
	}

	payload := map[string]interface{}{
		"supplier_id": req.SupplierID.String(),
		"notes":       req.Notes,
		"properties":  req.Properties,
	}
	res := shared.FireBefore(ctx, "purchase_order.creating", payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	if notes, ok := shared.PatchString(res, "notes"); ok {
		req.Notes = notes
	}

	in := PurchaseOrderInput{
		SupplierID: req.SupplierID,
		OrderDate:  parseOrderDate(req.OrderDate),
		Notes:      req.Notes,
		Properties: shared.PropertiesFromPatch(res, req.Properties),
		Items:      items,
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	orderID, _, err := CreatePurchaseOrder(ctx, tx, in)
	if err != nil {
		writeOrderError(c, err)
		return
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("purchase_order.created", map[string]interface{}{"order_id": orderID.String()})
	shared.JSONCreated(c, gin.H{"ok": true, "id": orderID})
}

func (h *Handler) PurchaseDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}

	order, err := getPurchaseOrder(ctx, h.db, id)
	if err != nil {
		shared.JSONNotFound(c, "订单不存在")
		return
	}

	itemRows, err := h.db.QueryContext(ctx, `
		SELECT poi.id, COALESCE(poi.order_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(poi.product_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       poi.quantity, COALESCE(poi.unit_price, 0), COALESCE(poi.amount, 0), COALESCE(poi.tax_rate, 0)
		FROM purchase_order_items poi
		LEFT JOIN products p ON poi.product_id = p.id
		WHERE poi.order_id = $1`, id)
	if err != nil {
		shared.JSONNotFound(c, "订单不存在")
		return
	}
	defer itemRows.Close()

	var items []models.PurchaseOrderItem
	for itemRows.Next() {
		var it models.PurchaseOrderItem
		if err := itemRows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount, &it.TaxRate); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		items = append(items, it)
	}

	netAmt, taxAmt := purchaseLineTaxSum(items)
	shared.JSONOK(c, gin.H{"order": order, "items": shared.EmptySlice(items), "net_amount": netAmt, "tax_amount": taxAmt})
}

func (h *Handler) PurchaseConfirm(c *gin.Context) {
	h.runPurchaseAction(c, ConfirmPurchaseOrder, "purchase_order.confirming", "purchase_order.confirmed")
}

func (h *Handler) PurchaseReceive(c *gin.Context) {
	h.runPurchaseAction(c, ReceivePurchaseOrder, "purchase_order.receiving", "purchase_order.received")
}

func (h *Handler) PurchasePay(c *gin.Context) {
	h.runPurchaseAction(c, PayPurchaseOrder, "purchase_order.paying", "purchase_order.paid")
}

func (h *Handler) PurchaseCancel(c *gin.Context) {
	h.runPurchaseAction(c, CancelPurchaseOrder, "purchase_order.cancelling", "purchase_order.cancelled")
}

func (h *Handler) runPurchaseAction(c *gin.Context, fn func(context.Context, *sql.Tx, uuid.UUID) error, before, after string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	payload := map[string]interface{}{"order_id": id.String()}
	res := shared.FireBefore(ctx, before, payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	if err := fn(ctx, tx, id); err != nil {
		writeOrderError(c, err)
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, after, id, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, "财务自动过账失败，本次操作已回滚")
		return
	}
	if props := shared.PropertiesFromPatch(res, nil); len(props) > 0 {
		if err := shared.MergeProperties(ctx, tx, "purchase_orders", id, props); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter(after, payload)
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PurchaseDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	payload := map[string]interface{}{"order_id": id.String()}
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "purchase_order.deleting", payload)) {
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	if err := DeletePurchaseOrder(ctx, tx, id); err != nil {
		writeOrderError(c, err)
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, "purchase_order.deleted", id, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("purchase_order.deleted", payload)
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PurchaseSearchAPI(c *gin.Context) {
	ctx := c.Request.Context()
	query := c.Query("q")

	rows, err := h.db.QueryContext(ctx, `
		SELECT po.id, po.order_no, COALESCE(po.supplier_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(s.name, '') as supplier_name, COALESCE(po.status, 'draft'),
		       COALESCE(po.total_amount, 0), COALESCE(po.paid_amount, 0),
		       COALESCE(po.order_date, '1970-01-01'), COALESCE(po.notes, ''),
		       COALESCE(po.created_at, '1970-01-01'),
		       COALESCE(po.company_id, 'default'), COALESCE(po.properties, '{}')
		FROM purchase_orders po
		LEFT JOIN suppliers s ON po.supplier_id = s.id
		WHERE po.order_no LIKE '%' || $1 || '%'
		   OR s.name LIKE '%' || $1 || '%'
		ORDER BY po.created_at DESC
		LIMIT 20`, query)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var orders []models.PurchaseOrder
	for rows.Next() {
		var o models.PurchaseOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName,
			&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
			&o.CompanyID, &o.Properties); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		orders = append(orders, o)
	}
	shared.JSONOK(c, shared.EmptySlice(orders))
}

// splitLineTax 差额法：net = round(amount/(1+rate/100), 2)，tax = amount - net，恒有 net+tax=amount。
func splitLineTax(amount, rate decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	if amount.IsZero() {
		return decimal.Zero, decimal.Zero
	}
	if rate.IsZero() {
		return amount.Round(2), decimal.Zero
	}
	denom := decimal.NewFromInt(100).Add(rate)
	net := amount.Mul(decimal.NewFromInt(100)).Div(denom).Round(2)
	tax := amount.Sub(net).Round(2)
	return net, tax
}

func orderLineTaxSum(items []models.SalesOrderItem) (decimal.Decimal, decimal.Decimal) {
	var net, tax decimal.Decimal
	for _, it := range items {
		n, t := splitLineTax(it.Amount, it.TaxRate)
		net = net.Add(n)
		tax = tax.Add(t)
	}
	return net, tax
}

func purchaseLineTaxSum(items []models.PurchaseOrderItem) (decimal.Decimal, decimal.Decimal) {
	var net, tax decimal.Decimal
	for _, it := range items {
		n, t := splitLineTax(it.Amount, it.TaxRate)
		net = net.Add(n)
		tax = tax.Add(t)
	}
	return net, tax
}
