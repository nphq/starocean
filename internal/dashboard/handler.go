package dashboard

import (
	"context"
	"database/sql"
	"time"

	"github.com/gin-gonic/gin"
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

func (h *Handler) DashboardDataAPI(c *gin.Context) {
	data, err := getDashboard(c.Request.Context(), h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, data)
}

func (h *Handler) ChartDataAPI(c *gin.Context) {
	ctx := c.Request.Context()

	data, err := chartDataSQLite(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, shared.EmptySlice(data))
}

type monthData struct {
	Label     string  `json:"label"`
	Sales     float64 `json:"sales"`
	Purchases float64 `json:"purchases"`
}

// chartDataSQLite 生成近 12 个月的销售/采购汇总（GROUP BY 按月聚合）。
func chartDataSQLite(ctx context.Context, db *sql.DB) ([]monthData, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
	startStr := start.Format("2006-01-02")
	endStr := start.AddDate(0, 12, 0).Format("2006-01-02")

	// GROUP BY 一次性聚合，避免逐月查询（24 次 → 2 次）
	salesMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', order_date) AS m, COALESCE(SUM(total_amount), 0)
		 FROM sales_orders WHERE order_date >= ? AND order_date < ? AND status != 'cancelled' GROUP BY m`,
		startStr, endStr)
	if err != nil {
		return nil, err
	}
	purchaseMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', order_date) AS m, COALESCE(SUM(total_amount), 0)
		 FROM purchase_orders WHERE order_date >= ? AND order_date < ? AND status != 'cancelled' GROUP BY m`,
		startStr, endStr)
	if err != nil {
		return nil, err
	}

	result := make([]monthData, 0, 12)
	for i := 0; i < 12; i++ {
		label := start.AddDate(0, i, 0).Format("2006-01")
		result = append(result, monthData{
			Label:     label,
			Sales:     salesMap[label].InexactFloat64(),
			Purchases: purchaseMap[label].InexactFloat64(),
		})
	}
	return result, nil
}

func getDashboard(ctx context.Context, db *sql.DB) (*models.DashboardData, error) {
	var monthSales decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_amount), 0) FROM sales_orders WHERE order_date >= date('now','start of month') AND status != 'cancelled'`).Scan(&monthSales); err != nil {
		return nil, err
	}

	var pendingOrders int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE status = 'draft'`).Scan(&pendingOrders); err != nil {
		return nil, err
	}

	var activeCustomers int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT customer_id) FROM sales_orders WHERE customer_id IS NOT NULL`).Scan(&activeCustomers); err != nil {
		return nil, err
	}

	recentRows, err := db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(so.customer_id, '00000000-0000-0000-0000-000000000000'), COALESCE(c.name, ''),
		so.status, COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
		COALESCE(so.order_date, '1970-01-01'), COALESCE(so.delivery_date, '1970-01-01'),
		COALESCE(so.notes, ''), COALESCE(so.created_at, '1970-01-01'),
		COALESCE(so.company_id,'default'), COALESCE(so.properties,'{}')
		FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
		ORDER BY so.created_at DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer recentRows.Close()

	var recentOrders []models.SalesOrder
	for recentRows.Next() {
		var o models.SalesOrder
		if err := recentRows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
			&o.Status, &o.TotalAmount, &o.PaidAmount,
			&o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt,
			&o.CompanyID, &o.Properties); err != nil {
			return nil, err
		}
		recentOrders = append(recentOrders, o)
	}

	stockRows, err := db.QueryContext(ctx, `SELECT id, COALESCE(code,''), COALESCE(name,''), COALESCE(category,''), COALESCE(unit,''),
		COALESCE(sale_price,0) as sale_price, COALESCE(cost_price,0) as cost_price, COALESCE(safety_stock,0),
		COALESCE(current_stock,0), COALESCE(pricing_type,'standard'), COALESCE(shelf_life_days,0),
		COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM products
		WHERE is_low_stock`)
	if err != nil {
		return nil, err
	}
	defer stockRows.Close()

	var lowStockItems []models.Product
	for stockRows.Next() {
		var p models.Product
		if err := stockRows.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit,
			&p.SalePrice, &p.CostPrice, &p.SafetyStock,
			&p.CurrentStock, &p.PricingType, &p.ShelfLifeDays, &p.CreatedAt, &p.CompanyID, &p.Properties); err != nil {
			return nil, err
		}
		lowStockItems = append(lowStockItems, p)
	}

	return &models.DashboardData{
		MonthSales:            monthSales.StringFixed(2),
		PendingOrders:         pendingOrders,
		LowStockCount:         int64(len(lowStockItems)),
		ActiveCustomers:       activeCustomers,
		RecentOrders:          shared.EmptySlice(recentOrders),
		LowStockItems:         shared.EmptySlice(lowStockItems),
		PendingReimbursements: getCount(ctx, db, "SELECT COUNT(*) FROM reimbursements WHERE status = 'pending_approval'"),
	}, nil
}

func getCount(ctx context.Context, db *sql.DB, query string) int64 {
	var count int64
	_ = db.QueryRowContext(ctx, query).Scan(&count)
	return count
}
