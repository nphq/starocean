package web

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
	"github.com/nphq/starocean/view/vmodel"
	"github.com/shopspring/decimal"
)

func (h *Handler) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()
	dash, err := h.dashboardData(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败，请稍后重试")
		return
	}
	points, _ := h.monthPoints(ctx)
	h.renderPage(c, "工作台", pages.Dashboard(dash, points))
}

func (h *Handler) dashboardData(ctx context.Context) (*models.DashboardData, error) {
	db := h.db
	var monthSales decimal.Decimal
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_amount), 0) FROM sales_orders WHERE order_date >= date('now','start of month') AND status != 'cancelled'`).Scan(&monthSales)
	var pendingOrders, activeCustomers int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE status = 'draft'`).Scan(&pendingOrders)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT customer_id) FROM sales_orders WHERE customer_id IS NOT NULL`).Scan(&activeCustomers)

	var recentOrders []models.SalesOrder
	rows, err := db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(so.customer_id, '00000000-0000-0000-0000-000000000000'), COALESCE(c.name, ''),
		so.status, COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
		COALESCE(so.order_date, '1970-01-01'), COALESCE(so.delivery_date, '1970-01-01'),
		COALESCE(so.notes, ''), COALESCE(so.created_at, '1970-01-01'),
		COALESCE(so.company_id,'default'), COALESCE(so.properties,'{}')
		FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
		ORDER BY so.created_at DESC LIMIT 8`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var o models.SalesOrder
			if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
				&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.DeliveryDate,
				&o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err == nil {
				recentOrders = append(recentOrders, o)
			}
		}
	}

	var lowStockItems []models.Product
	srows, err := db.QueryContext(ctx, `SELECT id, COALESCE(code,''), COALESCE(name,''), COALESCE(category,''), COALESCE(unit,''),
		COALESCE(sale_price,0), COALESCE(cost_price,0), COALESCE(safety_stock,0), COALESCE(current_stock,0),
		COALESCE(pricing_type,'standard'), COALESCE(shelf_life_days,0), COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM products WHERE current_stock <= safety_stock ORDER BY current_stock LIMIT 8`)
	if err == nil {
		defer srows.Close()
		for srows.Next() {
			var p models.Product
			if err := srows.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit,
				&p.SalePrice, &p.CostPrice, &p.SafetyStock, &p.CurrentStock,
				&p.PricingType, &p.ShelfLifeDays, &p.CreatedAt, &p.CompanyID, &p.Properties); err == nil {
				lowStockItems = append(lowStockItems, p)
			}
		}
	}

	var pendingReimb int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reimbursements WHERE status = 'pending_approval'`).Scan(&pendingReimb)

	return &models.DashboardData{
		MonthSales:            monthSales.StringFixed(2),
		PendingOrders:         pendingOrders,
		LowStockCount:         int64(len(lowStockItems)),
		ActiveCustomers:       activeCustomers,
		RecentOrders:          recentOrders,
		LowStockItems:         lowStockItems,
		PendingReimbursements: pendingReimb,
	}, nil
}

func (h *Handler) monthPoints(ctx context.Context) ([]vmodel.MonthPoint, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
	startStr := start.Format("2006-01-02")
	endStr := start.AddDate(0, 12, 0).Format("2006-01-02")
	salesMap, err := shared.SumByMonthSQLite(ctx, h.db,
		`SELECT strftime('%Y-%m', order_date) AS m, COALESCE(SUM(total_amount), 0)
		 FROM sales_orders WHERE order_date >= ? AND order_date < ? AND status != 'cancelled' GROUP BY m`, startStr, endStr)
	if err != nil {
		return nil, err
	}
	purMap, err := shared.SumByMonthSQLite(ctx, h.db,
		`SELECT strftime('%Y-%m', order_date) AS m, COALESCE(SUM(total_amount), 0)
		 FROM purchase_orders WHERE order_date >= ? AND order_date < ? AND status != 'cancelled' GROUP BY m`, startStr, endStr)
	if err != nil {
		return nil, err
	}
	out := make([]vmodel.MonthPoint, 0, 12)
	for i := 0; i < 12; i++ {
		label := start.AddDate(0, i, 0).Format("2006-01")
		out = append(out, vmodel.MonthPoint{Label: label, Sales: salesMap[label].InexactFloat64(), Purchases: purMap[label].InexactFloat64()})
	}
	return out, nil
}
