package web

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/orders"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
)

const purchasesListCols = `po.id, po.order_no, COALESCE(po.supplier_id, '00000000-0000-0000-0000-000000000000'),
       COALESCE(s.name, '') as supplier_name, COALESCE(po.status, 'draft'),
       COALESCE(po.total_amount, 0), COALESCE(po.paid_amount, 0),
       COALESCE(po.order_date, '1970-01-01'), COALESCE(po.notes, ''),
       COALESCE(po.created_at, '1970-01-01'),
       COALESCE(po.company_id, 'default'), COALESCE(po.properties::text, '{}')`

func scanPurchaseRow(row interface{ Scan(...any) error }) (models.PurchaseOrder, error) {
	var o models.PurchaseOrder
	err := row.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName,
		&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
		&o.CompanyID, &o.Properties)
	return o, err
}

func (h *Handler) PurchasesPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.PurchaseOrder
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+purchasesListCols+`
			FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id
			WHERE po.order_no ILIKE '%' || $1 || '%' OR s.name ILIKE '%' || $1 || '%'
			ORDER BY po.created_at DESC LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if o, err := scanPurchaseRow(rows); err == nil {
				items = append(items, o)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+purchasesListCols+`
			FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id
			ORDER BY po.created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if o, err := scanPurchaseRow(rows); err == nil {
				items = append(items, o)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchase_orders`).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.PurchaseListInner(items, q, page, total, limit))
		return
	}
	h.renderPage(c, "采购入库", pages.PurchaseList(items, q, page, total, limit))
}

func (h *Handler) supplierOptions(ctx context.Context) []models.Supplier {
	rows, err := h.db.QueryContext(ctx, `SELECT id, code, name, '', '', '', '', 0, 0, 0, 0,
		'1970-01-01', 'default', '{}' FROM suppliers ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.Supplier
	for rows.Next() {
		var s models.Supplier
		if err := rows.Scan(&s.ID, &s.Code, &s.Name, &s.ContactPerson, &s.Phone, &s.Email,
			&s.Address, &s.Balance, &s.Rating, &s.OnTimeRate, &s.QualityRate,
			&s.CreatedAt, &s.CompanyID, &s.Properties); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func (h *Handler) PurchaseNewPage(c *gin.Context) {
	h.renderPage(c, "新建采购订单", pages.PurchaseNewForm(h.supplierOptions(c.Request.Context()), ""))
}

func (h *Handler) PurchaseCreate(c *gin.Context) {
	ctx := c.Request.Context()
	supplierID, err := uuid.Parse(c.PostForm("supplier_id"))
	if err != nil {
		h.renderPage(c, "新建采购订单", pages.PurchaseNewForm(h.supplierOptions(ctx), "请选择供应商"))
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	orderID, _, err := orders.CreatePurchaseOrder(ctx, tx, orders.PurchaseOrderInput{
		SupplierID: supplierID,
		OrderDate:  parseDateOrNow(c.PostForm("order_date")),
		Notes:      strings.TrimSpace(c.PostForm("notes")),
		Items:      nil,
	})
	if err != nil {
		if isOrderBusinessErr(err) {
			h.renderPage(c, "新建采购订单", pages.PurchaseNewForm(h.supplierOptions(ctx), err.Error()))
			return
		}
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/purchases/"+orderID.String())
}

type purchaseDetailData struct {
	Order    models.PurchaseOrder
	Items    []models.PurchaseOrderItem
	Products []models.Product
	Error    string
}

func (h *Handler) loadPurchaseDetail(ctx context.Context, id uuid.UUID) (purchaseDetailData, error) {
	var d purchaseDetailData
	oo, err := scanPurchaseRow(h.db.QueryRowContext(ctx, `SELECT id, order_no, COALESCE(supplier_id, '00000000-0000-0000-0000-000000000000'), COALESCE(s.name,''),
		COALESCE(po.status, 'draft'), COALESCE(po.total_amount, 0), COALESCE(po.paid_amount, 0),
		COALESCE(po.order_date, '1970-01-01'), COALESCE(po.notes, ''), COALESCE(po.created_at, '1970-01-01'),
		COALESCE(po.company_id, 'default'), COALESCE(po.properties::text, '{}')
		FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id WHERE po.id = $1`, id))
	if err != nil {
		return d, err
	}
	d.Order = oo
	rows, err := h.db.QueryContext(ctx, `
		SELECT poi.id, COALESCE(poi.order_id, '00000000-0000-0000-0000-000000000000'),
		       COALESCE(poi.product_id, '00000000-0000-0000-0000-000000000000'),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       poi.quantity, COALESCE(poi.unit_price, 0), COALESCE(poi.amount, 0)
		FROM purchase_order_items poi LEFT JOIN products p ON poi.product_id = p.id
		WHERE poi.order_id = $1`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it models.PurchaseOrderItem
			if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.ProductName, &it.ProductCode,
				&it.Quantity, &it.UnitPrice, &it.Amount); err == nil {
				d.Items = append(d.Items, it)
			}
		}
	}
	d.Products = h.productOptions(ctx)
	return d, nil
}

func (h *Handler) PurchaseDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	d, err := h.loadPurchaseDetail(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "订单不存在")
		return
	}
	h.renderPage(c, d.Order.OrderNo, pages.PurchaseDetail(d.Order, d.Items, d.Products, d.Error))
}

func (h *Handler) PurchaseAddItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	productID, err := uuid.Parse(c.PostForm("product_id"))
	qty, _ := strconv.ParseInt(c.PostForm("quantity"), 10, 32)
	price := strings.TrimSpace(c.PostForm("unit_price"))
	fail := func(msg string) {
		d, derr := h.loadPurchaseDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = msg
		h.renderPage(c, d.Order.OrderNo, pages.PurchaseDetail(d.Order, d.Items, d.Products, d.Error))
	}
	if err != nil || qty <= 0 || qty > 1000000 {
		fail("商品与数量（1~1000000）不能为空")
		return
	}
	if _, err := shared.ParsePrice(price); err != nil {
		fail("单价格式无效")
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM purchase_orders WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		fail("订单不存在")
		return
	}
	if status != "draft" {
		fail("仅草稿单可以添加明细")
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4, $5)`, uuid.New(), id, productID, int32(qty), price); err != nil {
		fail("添加明细失败")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_orders SET total_amount = (SELECT COALESCE(SUM(amount),0) FROM purchase_order_items WHERE order_id = $1), updated_at = NOW() WHERE id = $1`, id); err != nil {
		fail("添加明细失败")
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/purchases/"+id.String())
}

func (h *Handler) runPurchaseAction(c *gin.Context, fn func(context.Context, *sql.Tx, uuid.UUID) error, after string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	if err := fn(ctx, tx, id); err != nil {
		d, derr := h.loadPurchaseDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = err.Error()
		if !isOrderBusinessErr(err) {
			d.Error = "操作失败，请稍后重试"
		}
		h.renderPage(c, d.Order.OrderNo, pages.PurchaseDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, after, id, "web"); err != nil {
		d, derr := h.loadPurchaseDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = "财务自动过账失败，本次操作已回滚"
		h.renderPage(c, d.Order.OrderNo, pages.PurchaseDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/purchases/"+id.String())
}

func (h *Handler) PurchaseConfirm(c *gin.Context) {
	h.runPurchaseAction(c, orders.ConfirmPurchaseOrder, "purchase_order.confirmed")
}
func (h *Handler) PurchaseReceive(c *gin.Context) {
	h.runPurchaseAction(c, orders.ReceivePurchaseOrder, "purchase_order.received")
}
func (h *Handler) PurchasePay(c *gin.Context) {
	h.runPurchaseAction(c, orders.PayPurchaseOrder, "purchase_order.paid")
}
func (h *Handler) PurchaseCancel(c *gin.Context) {
	h.runPurchaseAction(c, orders.CancelPurchaseOrder, "purchase_order.cancelled")
}

func (h *Handler) PurchaseDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	if err := orders.DeletePurchaseOrder(ctx, tx, id); err != nil {
		d, derr := h.loadPurchaseDetail(ctx, id)
		if derr != nil {
			redirect(c, "/purchases")
			return
		}
		d.Error = err.Error()
		if !isOrderBusinessErr(err) {
			d.Error = "操作失败，请稍后重试"
		}
		h.renderPage(c, d.Order.OrderNo, pages.PurchaseDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	_ = ledger.OnBusinessEvent(ctx, tx, "purchase_order.deleted", id, "web")
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/purchases")
}
