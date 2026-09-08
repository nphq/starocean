package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/view/pages"
	"github.com/shopspring/decimal"
)

func (h *Handler) InventoryPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	rows, err := h.db.QueryContext(ctx, `SELECT im.id, im.product_id, COALESCE(p.name,''), COALESCE(p.code,''),
		im.type, im.quantity, COALESCE(im.reference_type,''),
		im.before_stock, im.after_stock, COALESCE(im.created_at,'1970-01-01'),
		COALESCE(im.company_id,'default')
		FROM inventory_movements im LEFT JOIN products p ON im.product_id = p.id
		ORDER BY im.created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.InventoryMovement
	for rows.Next() {
		var m models.InventoryMovement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductName, &m.ProductCode,
			&m.Type, &m.Quantity, &m.ReferenceType, &m.BeforeStock, &m.AfterStock,
			&m.CreatedAt, &m.CompanyID); err == nil {
			items = append(items, m)
		}
	}
	var total int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_movements").Scan(&total)
	if isHX(c) {
		renderFrag(c, pages.InventoryListInner(items, page, total, limit))
		return
	}
	h.renderPage(c, "库存变动", pages.InventoryList(items, page, total, limit))
}

func (h *Handler) InventoryByProduct(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("productId"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	p := mustProduct(c, h.db, productID)
	if p.ID == uuid.Nil {
		c.String(http.StatusNotFound, "商品不存在")
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT im.id, im.product_id, COALESCE(p.name,''), COALESCE(p.code,''),
		im.type, im.quantity, COALESCE(im.reference_type,''),
		im.before_stock, im.after_stock, COALESCE(im.created_at,'1970-01-01'),
		COALESCE(im.company_id,'default')
		FROM inventory_movements im LEFT JOIN products p ON im.product_id = p.id
		WHERE im.product_id = $1 ORDER BY im.created_at DESC LIMIT 100`, productID)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.InventoryMovement
	for rows.Next() {
		var m models.InventoryMovement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductName, &m.ProductCode,
			&m.Type, &m.Quantity, &m.ReferenceType, &m.BeforeStock, &m.AfterStock,
			&m.CreatedAt, &m.CompanyID); err == nil {
			items = append(items, m)
		}
	}
	h.renderPage(c, p.Name+" · 库存流水", pages.InventoryByProduct(p, items))
}

func (h *Handler) FinancePage(c *gin.Context) {
	ctx := c.Request.Context()
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	rows, err := h.db.QueryContext(ctx, `SELECT id, type, amount, COALESCE(partner_name,''), COALESCE(notes,''),
		COALESCE(payment_date,'1970-01-01'), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM payments ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.Payment
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.ID, &p.Type, &p.Amount, &p.PartnerName, &p.Notes,
			&p.PaymentDate, &p.CreatedAt, &p.CompanyID); err == nil {
			items = append(items, p)
		}
	}
	var total int64
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments`).Scan(&total)
	var income, expense, receivable, payable decimal.Decimal
	_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(amount AS numeric)), 0) FROM payments WHERE type = '收入' AND payment_date >= DATE_TRUNC('month', CURRENT_DATE)::date`).Scan(&income)
	_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric)), 0) FROM purchase_orders WHERE order_date >= DATE_TRUNC('month', CURRENT_DATE)::date AND status != 'cancelled'`).Scan(&expense)
	_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) FROM sales_orders WHERE status != 'cancelled'`).Scan(&receivable)
	_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) FROM purchase_orders WHERE status != 'cancelled'`).Scan(&payable)
	summary := pages.FinanceSummary{
		Income: income.StringFixed(2), Expense: expense.StringFixed(2),
		Receivable: receivable.StringFixed(2), Payable: payable.StringFixed(2),
	}
	if isHX(c) {
		renderFrag(c, pages.PaymentListInner(items, page, total, limit))
		return
	}
	h.renderPage(c, "收付流水", pages.FinanceHome(summary, items, page, total, limit, ""))
}

func (h *Handler) PaymentCreate(c *gin.Context) {
	ctx := c.Request.Context()
	payType := c.PostForm("type")
	amountStr := c.PostForm("amount")
	fail := func(msg string) {
		h.renderPage(c, "收付流水", pages.FinanceHome(pages.FinanceSummary{}, nil, 1, 0, 20, msg))
	}
	if payType != "收入" && payType != "支出" {
		fail("类型必须为收入或支出")
		return
	}
	amt, err := decimal.NewFromString(amountStr)
	if err != nil || amt.LessThanOrEqual(decimal.Zero) {
		fail("金额必须为正数")
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO payments (id, type, amount, partner_name, notes, payment_date, created_at)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NOW(), NOW())`,
		uuid.New(), payType, amountStr, c.PostForm("partner_name"), c.PostForm("notes")); err != nil {
		fail("保存失败")
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/finance")
}
