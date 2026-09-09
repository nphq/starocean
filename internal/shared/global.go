package shared

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nphq/starocean/internal/models"
	"github.com/xuri/excelize/v2"
)

func GlobalSearchAPI(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		qStr := c.Query("q")
		if qStr == "" {
			c.JSON(http.StatusOK, gin.H{
				"products":        []models.Product{},
				"customers":       []models.Customer{},
				"suppliers":       []models.Supplier{},
				"sales_orders":    []models.SalesOrder{},
				"purchase_orders": []models.PurchaseOrder{},
			})
			return
		}
		ctx := c.Request.Context()
		pattern := "%" + qStr + "%"

		var (
			prods     []models.Product
			custs     []models.Customer
			supps     []models.Supplier
			sales     []models.SalesOrder
			purchases []models.PurchaseOrder
			wg        sync.WaitGroup
		)
		wg.Add(5)
		go func() { defer wg.Done(); prods = searchProducts(ctx, db, pattern) }()
		go func() { defer wg.Done(); custs = searchCustomers(ctx, db, pattern) }()
		go func() { defer wg.Done(); supps = searchSuppliers(ctx, db, pattern) }()
		go func() { defer wg.Done(); sales = searchSalesOrders(ctx, db, pattern) }()
		go func() { defer wg.Done(); purchases = searchPurchaseOrders(ctx, db, pattern) }()
		wg.Wait()

		c.JSON(http.StatusOK, gin.H{
			"products":        prods,
			"customers":       custs,
			"suppliers":       supps,
			"sales_orders":    sales,
			"purchase_orders": purchases,
		})
	}
}

func searchProducts(ctx context.Context, db *sql.DB, pattern string) []models.Product {
	rows, err := db.QueryContext(ctx, `SELECT id, code, name, COALESCE(category,''), COALESCE(unit,''),
		COALESCE(sale_price,0), COALESCE(cost_price,0), COALESCE(safety_stock,0),
		COALESCE(current_stock,0), COALESCE(pricing_type,'standard'),
		COALESCE(shelf_life_days,0),
		COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM products WHERE search_text LIKE $1 ORDER BY name LIMIT 20`, pattern)
	if err != nil {
		return []models.Product{}
	}
	defer rows.Close()
	var items []models.Product
	for rows.Next() {
		var p models.Product
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit, &p.SalePrice, &p.CostPrice, &p.SafetyStock, &p.CurrentStock, &p.PricingType, &p.ShelfLifeDays, &p.CreatedAt, &p.CompanyID, &p.Properties); err != nil {
			continue
		}
		items = append(items, p)
	}
	return items
}

func searchCustomers(ctx context.Context, db *sql.DB, pattern string) []models.Customer {
	rows, err := db.QueryContext(ctx, `SELECT id, code, name, COALESCE(contact_person,''), COALESCE(phone,''),
		COALESCE(email,''), COALESCE(address,''), COALESCE(credit_limit, 0), COALESCE(balance, 0),
		COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM customers WHERE name LIKE $1 OR code LIKE $1 ORDER BY name LIMIT 20`, pattern)
	if err != nil {
		return []models.Customer{}
	}
	defer rows.Close()
	var items []models.Customer
	for rows.Next() {
		var c models.Customer
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.ContactPerson, &c.Phone, &c.Email, &c.Address, &c.CreditLimit, &c.Balance, &c.CreatedAt, &c.CompanyID, &c.Properties); err != nil {
			continue
		}
		items = append(items, c)
	}
	return items
}

func searchSuppliers(ctx context.Context, db *sql.DB, pattern string) []models.Supplier {
	rows, err := db.QueryContext(ctx, `SELECT id, code, name, COALESCE(contact_person,''), COALESCE(phone,''),
		COALESCE(email,''), COALESCE(address,''), COALESCE(balance, 0), COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM suppliers WHERE name LIKE $1 OR code LIKE $1 ORDER BY name LIMIT 20`, pattern)
	if err != nil {
		return []models.Supplier{}
	}
	defer rows.Close()
	var items []models.Supplier
	for rows.Next() {
		var s models.Supplier
		if err := rows.Scan(&s.ID, &s.Code, &s.Name, &s.ContactPerson, &s.Phone, &s.Email, &s.Address, &s.Balance, &s.CreatedAt, &s.CompanyID, &s.Properties); err != nil {
			continue
		}
		items = append(items, s)
	}
	return items
}

func searchSalesOrders(ctx context.Context, db *sql.DB, pattern string) []models.SalesOrder {
	rows, err := db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(so.customer_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		COALESCE(c.name,''), COALESCE(so.status,'draft'), COALESCE(so.total_amount, 0),
		COALESCE(so.paid_amount, 0), COALESCE(so.order_date,'1970-01-01'),
		COALESCE(so.delivery_date,'1970-01-01'), COALESCE(so.notes,''), COALESCE(so.created_at,'1970-01-01'),
		COALESCE(so.company_id,'default'), COALESCE(so.properties,'{}')
		FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
		WHERE so.order_no LIKE $1 OR c.name LIKE $1 ORDER BY so.created_at DESC LIMIT 20`, pattern)
	if err != nil {
		return []models.SalesOrder{}
	}
	defer rows.Close()
	var items []models.SalesOrder
	for rows.Next() {
		var o models.SalesOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName, &o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err != nil {
			continue
		}
		items = append(items, o)
	}
	return items
}

func searchPurchaseOrders(ctx context.Context, db *sql.DB, pattern string) []models.PurchaseOrder {
	rows, err := db.QueryContext(ctx, `SELECT po.id, po.order_no, COALESCE(po.supplier_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		COALESCE(s.name,''), COALESCE(po.status,'draft'), COALESCE(po.total_amount, 0),
		COALESCE(po.paid_amount, 0), COALESCE(po.order_date,'1970-01-01'),
		COALESCE(po.delivery_date,'1970-01-01'), COALESCE(po.notes,''), COALESCE(po.created_at,'1970-01-01'),
		COALESCE(po.company_id,'default'), COALESCE(po.properties,'{}')
		FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id
		WHERE po.order_no LIKE $1 OR s.name LIKE $1 ORDER BY po.created_at DESC LIMIT 20`, pattern)
	if err != nil {
		return []models.PurchaseOrder{}
	}
	defer rows.Close()
	var items []models.PurchaseOrder
	for rows.Next() {
		var o models.PurchaseOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName, &o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err != nil {
			continue
		}
		items = append(items, o)
	}
	return items
}

func SalesExportXLSX(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		orders := []models.SalesOrder{}
		rows, err := db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(so.customer_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
			COALESCE(c.name,''), COALESCE(so.status,'draft'), COALESCE(so.total_amount, 0),
			COALESCE(so.paid_amount, 0), COALESCE(so.order_date,'1970-01-01'),
			COALESCE(so.delivery_date,'1970-01-01'), COALESCE(so.notes,''), COALESCE(so.created_at,'1970-01-01'),
			COALESCE(so.company_id,'default'), COALESCE(so.properties,'{}')
			FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
			ORDER BY so.created_at DESC LIMIT 1000`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var o models.SalesOrder
			if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName, &o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err != nil {
				continue
			}
			orders = append(orders, o)
		}

		f := excelize.NewFile()
		sheet := "Sales Orders"
		_ = f.SetSheetName("Sheet1", sheet)
		headers := []string{"订单号", "客户", "日期", "金额", "状态"}
		for i, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			_ = f.SetCellValue(sheet, cell, h)
		}
		for i, o := range orders {
			row := i + 2
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), o.OrderNo)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), o.CustomerName)
			if o.OrderDate != (time.Time{}) {
				_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), o.OrderDate.Format("2006-01-02"))
			}
			_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), o.TotalAmount.StringFixed(2))
			_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), o.Status)
		}
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Header("Content-Disposition", "attachment; filename=sales_orders.xlsx")
		// 响应头已写出，写入失败无法回传，仅显式忽略。
		_ = f.Write(c.Writer)
	}
}
