package web

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/orders"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
)

const salesListCols = `so.id, so.order_no, COALESCE(so.customer_id, '00000000-0000-0000-0000-000000000000'),
       COALESCE(c.name, '') as customer_name, COALESCE(so.status, 'draft'),
       COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
       COALESCE(so.order_date, '1970-01-01'), COALESCE(so.notes, ''),
       COALESCE(so.created_at, '1970-01-01'),
       COALESCE(so.company_id, 'default'), COALESCE(so.properties::text, '{}')`

func scanSalesRow(row interface{ Scan(...any) error }) (models.SalesOrder, error) {
	var o models.SalesOrder
	err := row.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
		&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.Notes, &o.CreatedAt,
		&o.CompanyID, &o.Properties)
	return o, err
}

func (h *Handler) SalesPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.SalesOrder
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+salesListCols+`
			FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
			WHERE so.order_no ILIKE '%' || $1 || '%' OR c.name ILIKE '%' || $1 || '%'
			ORDER BY so.created_at DESC LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if o, err := scanSalesRow(rows); err == nil {
				items = append(items, o)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+salesListCols+`
			FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
			ORDER BY so.created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if o, err := scanSalesRow(rows); err == nil {
				items = append(items, o)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders`).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.SaleListInner(items, q, page, total, limit))
		return
	}
	custs := h.customerOptions(ctx)
	h.renderPage(c, "销售订单", pages.SaleList(items, q, page, total, limit, custs))
}

func (h *Handler) customerOptions(ctx context.Context) []models.Customer {
	rows, err := h.db.QueryContext(ctx, `SELECT id, code, name, '', '', '', '', 0, 0, 'normal', '',
		'1970-01-01', 'default', '{}' FROM customers ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.Customer
	for rows.Next() {
		var cu models.Customer
		if err := rows.Scan(&cu.ID, &cu.Code, &cu.Name, &cu.ContactPerson, &cu.Phone, &cu.Email,
			&cu.Address, &cu.CreditLimit, &cu.Balance, &cu.Tier, &cu.SalesPerson,
			&cu.CreatedAt, &cu.CompanyID, &cu.Properties); err == nil {
			out = append(out, cu)
		}
	}
	return out
}

func (h *Handler) SaleNewPage(c *gin.Context) {
	h.renderPage(c, "新建销售订单", pages.SaleNewForm(h.customerOptions(c.Request.Context()), ""))
}

func parseDateOrNow(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Now()
}

func (h *Handler) SaleCreate(c *gin.Context) {
	ctx := c.Request.Context()
	customerID, err := uuid.Parse(c.PostForm("customer_id"))
	if err != nil {
		h.renderPage(c, "新建销售订单", pages.SaleNewForm(h.customerOptions(ctx), "请选择客户"))
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	orderID, _, err := orders.CreateSalesOrder(ctx, tx, orders.SalesOrderInput{
		CustomerID: customerID,
		OrderDate:  parseDateOrNow(c.PostForm("order_date")),
		Notes:      strings.TrimSpace(c.PostForm("notes")),
		Items:      nil,
	})
	if err != nil {
		if isOrderBusinessErr(err) {
			h.renderPage(c, "新建销售订单", pages.SaleNewForm(h.customerOptions(ctx), err.Error()))
			return
		}
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/sales/"+orderID.String())
}

func isOrderBusinessErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, k := range []string{"库存不足", "超出客户信用额度", "cannot transition", "cannot cancel", "customer not found", "数量必须", "仅草稿", "请先取消", "不能删除", "存在关联"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

type saleDetailData struct {
	Order    models.SalesOrder
	Items    []models.SalesOrderItem
	Products []models.Product
	Error    string
}

func (h *Handler) loadSaleDetail(ctx context.Context, id uuid.UUID) (saleDetailData, error) {
	var d saleDetailData
	var o models.SalesOrder
	err := h.db.QueryRowContext(ctx, `SELECT id, order_no, COALESCE(customer_id, '00000000-0000-0000-0000-000000000000'),
		COALESCE(status, 'draft'), COALESCE(total_amount, 0), COALESCE(paid_amount, 0),
		COALESCE(order_date, '1970-01-01'), COALESCE(delivery_date, '1970-01-01'), COALESCE(notes, ''), COALESCE(created_at, '1970-01-01'),
		COALESCE(company_id, 'default'), COALESCE(properties::text, '{}') FROM sales_orders WHERE id = $1`, id).Scan(
		&o.ID, &o.OrderNo, &o.CustomerID, &o.Status, &o.TotalAmount, &o.PaidAmount,
		&o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties)
	if err != nil {
		return d, err
	}
	var cname string
	_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(name,'') FROM customers WHERE id = $1`, o.CustomerID).Scan(&cname)
	o.CustomerName = cname
	d.Order = o
	rows, err := h.db.QueryContext(ctx, `
		SELECT soi.id, COALESCE(soi.order_id, '00000000-0000-0000-0000-000000000000'),
		       COALESCE(soi.product_id, '00000000-0000-0000-0000-000000000000'),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       soi.quantity, COALESCE(soi.unit_price, 0), COALESCE(soi.amount, 0)
		FROM sales_order_items soi LEFT JOIN products p ON soi.product_id = p.id
		WHERE soi.order_id = $1`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it models.SalesOrderItem
			if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.ProductName, &it.ProductCode,
				&it.Quantity, &it.UnitPrice, &it.Amount); err == nil {
				d.Items = append(d.Items, it)
			}
		}
	}
	d.Products = h.productOptions(ctx)
	return d, nil
}

func (h *Handler) SaleDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	d, err := h.loadSaleDetail(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "订单不存在")
		return
	}
	h.renderPage(c, d.Order.OrderNo, pages.SaleDetail(d.Order, d.Items, d.Products, d.Error))
}

// SaleAddItem 明细逐行添加（仅草稿单）。
func (h *Handler) SaleAddItem(c *gin.Context) {
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
		d, derr := h.loadSaleDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = msg
		h.renderPage(c, d.Order.OrderNo, pages.SaleDetail(d.Order, d.Items, d.Products, d.Error))
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
	if err := tx.QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		fail("订单不存在")
		return
	}
	if status != "draft" {
		fail("仅草稿单可以添加明细")
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sales_order_items (id, order_id, product_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4, $5)`, uuid.New(), id, productID, int32(qty), price); err != nil {
		fail("添加明细失败")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sales_orders SET total_amount = (SELECT COALESCE(SUM(amount),0) FROM sales_order_items WHERE order_id = $1), updated_at = NOW() WHERE id = $1`, id); err != nil {
		fail("添加明细失败")
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/sales/"+id.String())
}

func (h *Handler) runSaleAction(c *gin.Context, fn func(context.Context, *sql.Tx, uuid.UUID) error, after string) {
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
		d, derr := h.loadSaleDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = err.Error()
		if !isOrderBusinessErr(err) {
			d.Error = "操作失败，请稍后重试"
		}
		h.renderPage(c, d.Order.OrderNo, pages.SaleDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	if err := ledger.OnBusinessEvent(ctx, tx, after, id, "web"); err != nil {
		d, derr := h.loadSaleDetail(ctx, id)
		if derr != nil {
			c.String(http.StatusNotFound, "订单不存在")
			return
		}
		d.Error = "财务自动过账失败，本次操作已回滚"
		h.renderPage(c, d.Order.OrderNo, pages.SaleDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/sales/"+id.String())
}

func (h *Handler) SaleConfirm(c *gin.Context) {
	h.runSaleAction(c, orders.ConfirmSalesOrder, "sales_order.confirmed")
}
func (h *Handler) SaleShip(c *gin.Context) {
	h.runSaleAction(c, orders.ShipSalesOrder, "sales_order.shipped")
}
func (h *Handler) SaleInvoice(c *gin.Context) {
	h.runSaleAction(c, orders.InvoiceSalesOrder, "sales_order.invoiced")
}
func (h *Handler) SaleCancel(c *gin.Context) {
	h.runSaleAction(c, orders.CancelSalesOrder, "sales_order.cancelled")
}

func (h *Handler) SaleDelete(c *gin.Context) {
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
	if err := orders.DeleteSalesOrder(ctx, tx, id); err != nil {
		d, derr := h.loadSaleDetail(ctx, id)
		if derr != nil {
			redirect(c, "/sales")
			return
		}
		d.Error = err.Error()
		if !isOrderBusinessErr(err) {
			d.Error = "操作失败，请稍后重试"
		}
		h.renderPage(c, d.Order.OrderNo, pages.SaleDetail(d.Order, d.Items, d.Products, d.Error))
		return
	}
	_ = ledger.OnBusinessEvent(ctx, tx, "sales_order.deleted", id, "web")
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/sales")
}
