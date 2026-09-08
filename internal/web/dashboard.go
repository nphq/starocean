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
	h.renderPage(c, "仪表盘", pages.Dashboard(dash, points))
}

func (h *Handler) dashboardData(ctx context.Context) (*models.DashboardData, error) {
	db := h.db
	var monthSales decimal.Decimal
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_amount), 0) FROM sales_orders WHERE order_date >= DATE_TRUNC('month', CURRENT_DATE)::date AND status != 'cancelled'`).Scan(&monthSales)
	var pendingOrders, activeCustomers int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE status = 'draft'`).Scan(&pendingOrders)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT customer_id) FROM sales_orders WHERE customer_id IS NOT NULL`).Scan(&activeCustomers)

	var recentOrders []models.SalesOrder
	rows, err := db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(so.customer_id, '00000000-0000-0000-0000-000000000000'), COALESCE(c.name, ''),
		so.status, COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
		COALESCE(so.order_date, '1970-01-01'), COALESCE(so.delivery_date, '1970-01-01'),
		COALESCE(so.notes, ''), COALESCE(so.created_at, '1970-01-01'),
		COALESCE(so.company_id,'default'), COALESCE(so.properties::text,'{}')
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
		COALESCE(company_id,'default'), COALESCE(properties::text,'{}')
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

	var pendingReimb, activeEmp, expiringContracts int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reimbursements WHERE status = 'pending_approval'`).Scan(&pendingReimb)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM employees WHERE status != 'inactive'`).Scan(&activeEmp)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contracts WHERE end_date >= CURRENT_DATE AND end_date <= CURRENT_DATE + INTERVAL '30 days' AND status = 'active'`).Scan(&expiringContracts)

	return &models.DashboardData{
		MonthSales:            monthSales.StringFixed(2),
		PendingOrders:         pendingOrders,
		LowStockCount:         int64(len(lowStockItems)),
		ActiveCustomers:       activeCustomers,
		RecentOrders:          recentOrders,
		LowStockItems:         lowStockItems,
		PendingReimbursements: pendingReimb,
		ActiveEmployees:       activeEmp,
		ExpiringContracts:     expiringContracts,
	}, nil
}

func (h *Handler) monthPoints(ctx context.Context) ([]vmodel.MonthPoint, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
	if shared.IsSQLite() {
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
	rows, err := h.db.QueryContext(ctx, `
		SELECT TO_CHAR(d.month, 'YYYY-MM') AS label, COALESCE(s.sales, 0), COALESCE(p.purchases, 0)
		FROM generate_series(DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '11 months', DATE_TRUNC('month', CURRENT_DATE), '1 month') AS d(month)
		LEFT JOIN LATERAL (SELECT COALESCE(SUM(total_amount), 0) AS sales FROM sales_orders
			WHERE order_date >= d.month AND order_date < d.month + INTERVAL '1 month' AND status != 'cancelled') s ON true
		LEFT JOIN LATERAL (SELECT COALESCE(SUM(total_amount), 0) AS purchases FROM purchase_orders
			WHERE order_date >= d.month AND order_date < d.month + INTERVAL '1 month' AND status != 'cancelled') p ON true
		ORDER BY d.month`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []vmodel.MonthPoint
	for rows.Next() {
		var m vmodel.MonthPoint
		if err := rows.Scan(&m.Label, &m.Sales, &m.Purchases); err == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}
