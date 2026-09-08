package server

import (
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nphq/starocean/internal/attendance"
	"github.com/nphq/starocean/internal/customers"
	"github.com/nphq/starocean/internal/dashboard"
	"github.com/nphq/starocean/internal/finance"
	"github.com/nphq/starocean/internal/inventory"
	"github.com/nphq/starocean/internal/ledger"
	mw "github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/internal/orders"
	"github.com/nphq/starocean/internal/partnernotes"
	"github.com/nphq/starocean/internal/personnel"
	"github.com/nphq/starocean/internal/picking"
	"github.com/nphq/starocean/internal/print"
	"github.com/nphq/starocean/internal/products"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/internal/suppliers"
	"github.com/nphq/starocean/internal/web"
	"github.com/nphq/starocean/internal/workreports"
)

func RegisterRoutes(r *gin.Engine, db *sql.DB, publicFS embed.FS, companyName string) {
	prodH := products.New(db)
	custH := customers.New(db)
	supH := suppliers.New(db)
	orderH := orders.New(db)
	invH := inventory.New(db)
	finH := finance.New(db)
	dashH := dashboard.New(db)
	attendanceH := attendance.New(db)
	noteH := partnernotes.New(db)
	wrH := workreports.New(db)
	persH := personnel.New(db)
	printH := print.New(db, companyName)
	pickH := picking.New(db)
	ledgerH := ledger.New(db)

	r.POST("/api/login", mw.LoginHandler(db))
	r.POST("/api/logout", mw.LogoutHandler())
	r.GET("/api/me", mw.MeHandler(db))

	staticFS, err := fs.Sub(publicFS, "public")
	if err == nil {
		r.StaticFS("/static", http.FS(staticFS))
	} else {
		log.Printf("static files warning: %v", err)
	}

	auth := r.Group("/api")
	auth.Use(mw.AuthRequired(db))
	auth.Use(mw.WriteRequired(db))
	{
		auth.GET("/dashboard", dashH.DashboardDataAPI)
		auth.GET("/dashboard/chart", dashH.ChartDataAPI)

		auth.GET("/attendance", attendanceH.AttendancePage)
		auth.POST("/attendance/import", attendanceH.AttendanceImport)

		auth.GET("/products", prodH.ProductsPage)
		auth.POST("/products", prodH.ProductCreate)
		auth.GET("/products/search", prodH.ProductSearchAPI)
		auth.POST("/products/import", prodH.ImportProducts)
		auth.GET("/products/:id", prodH.ProductDetailPage)
		auth.PUT("/products/:id", prodH.ProductUpdate)
		auth.DELETE("/products/:id", prodH.ProductDelete)
		auth.POST("/products/:id/stock", prodH.ProductStockAdjust)
		auth.GET("/products/:id/price-tiers", prodH.PriceTiersAPI)
		auth.POST("/products/:id/price-tiers", prodH.PriceTierCreate)
		auth.DELETE("/products/:id/price-tiers/:tierId", prodH.PriceTierDelete)
		auth.GET("/products/:id/resolve-price", prodH.ResolvePriceAPI)

		auth.GET("/customers", custH.CustomersPage)
		auth.POST("/customers", custH.CustomerCreate)
		auth.GET("/customers/search", custH.CustomerSearchAPI)
		auth.GET("/customers/:id", custH.CustomerDetailPage)
		auth.PUT("/customers/:id", custH.CustomerUpdate)
		auth.DELETE("/customers/:id", custH.CustomerDelete)

		auth.GET("/suppliers", supH.SuppliersPage)
		auth.POST("/suppliers", supH.SupplierCreate)
		auth.GET("/suppliers/search", supH.SupplierSearchAPI)
		auth.GET("/suppliers/:id", supH.SupplierDetailPage)
		auth.PUT("/suppliers/:id", supH.SupplierUpdate)
		auth.DELETE("/suppliers/:id", supH.SupplierDelete)

		auth.GET("/sales", orderH.SalesPage)
		auth.POST("/sales", orderH.SalesCreate)
		auth.GET("/sales/search", orderH.SalesSearchAPI)
		auth.GET("/sales/export", shared.SalesExportXLSX(db))
		auth.GET("/sales/:id", orderH.SalesDetailPage)
		auth.POST("/sales/:id/confirm", orderH.SalesConfirm)
		auth.POST("/sales/:id/ship", orderH.SalesShip)
		auth.POST("/sales/:id/invoice", orderH.SalesInvoice)
		auth.POST("/sales/:id/cancel", orderH.SalesCancel)
		auth.DELETE("/sales/:id", orderH.SalesDelete)

		auth.GET("/purchases", orderH.PurchasesPage)
		auth.POST("/purchases", orderH.PurchaseCreate)
		auth.GET("/purchases/search", orderH.PurchaseSearchAPI)
		auth.GET("/purchases/:id", orderH.PurchaseDetailPage)
		auth.POST("/purchases/:id/confirm", orderH.PurchaseConfirm)
		auth.POST("/purchases/:id/receive", orderH.PurchaseReceive)
		auth.POST("/purchases/:id/pay", orderH.PurchasePay)
		auth.POST("/purchases/:id/cancel", orderH.PurchaseCancel)
		auth.DELETE("/purchases/:id", orderH.PurchaseDelete)

		auth.GET("/inventory", invH.InventoryPage)
		auth.GET("/inventory/:productId", invH.InventoryByProductPage)

		auth.GET("/finance", finH.FinancePage)
		auth.POST("/finance/payments", finH.PaymentCreate)
		auth.GET("/finance/payments/:id", finH.PaymentDetail)
		auth.GET("/finance/clearings", finH.ClearingsList)
		auth.GET("/finance/cashflow", finH.CashflowAPI)
		auth.GET("/finance/cashflow/trend", finH.CashflowTrendAPI)
		auth.GET("/finance/receivable/aging", finH.ReceivableAgingAPI)
		auth.GET("/finance/month-over-month", finH.MonthOverMonthAPI)

		auth.POST("/partner-notes", noteH.Create)
		auth.GET("/partner-notes/:partnerType/:partnerId", noteH.ListByPartner)
		auth.DELETE("/partner-notes/:id", noteH.Delete)

		auth.GET("/workreports", wrH.ReportsPage)
		auth.POST("/workreports", wrH.ReportCreate)
		auth.GET("/workreports/templates", wrH.TemplatesPage)
		auth.POST("/workreports/templates", wrH.TemplateCreate)
		auth.DELETE("/workreports/templates/:id", wrH.TemplateDelete)
		auth.POST("/workreports/weekly/generate", wrH.WeeklyGenerateCreate)
		auth.GET("/workreports/:id", wrH.ReportDetailPage)
		auth.PUT("/workreports/:id", wrH.ReportUpdate)

		auth.GET("/finance/reimbursements", finH.ReimbursementsPage)
		auth.POST("/finance/reimbursements", finH.ReimbursementCreate)
		auth.GET("/finance/reimbursements/:id", finH.ReimbursementDetailPage)
		auth.PUT("/finance/reimbursements/:id", finH.ReimbursementUpdate)
		auth.POST("/finance/reimbursements/:id/submit", finH.ReimbursementSubmit)
		auth.POST("/finance/reimbursements/:id/approve", finH.ReimbursementApprove)
		auth.POST("/finance/reimbursements/:id/reject", finH.ReimbursementReject)
		auth.POST("/finance/reimbursements/:id/pay", finH.ReimbursementPay)

		auth.GET("/finance/invoices", finH.InvoicesPage)
		auth.POST("/finance/invoices", finH.InvoiceCreate)
		auth.GET("/finance/invoices/:id", finH.InvoiceDetailPage)
		auth.POST("/finance/invoices/:id/void", finH.InvoiceVoid)

		auth.GET("/finance/reconciliations", finH.ReconciliationsPage)
		auth.POST("/finance/reconciliations", finH.ReconciliationCreate)
		auth.GET("/finance/reconciliation/preview", finH.ReconciliationPreview)
		auth.GET("/finance/reconciliations/:id", finH.ReconciliationDetailPage)
		auth.POST("/finance/reconciliations/:id/confirm", finH.ReconciliationConfirm)
		auth.GET("/finance/customers/search", finH.CustomersSearchAPI)
		auth.GET("/finance/suppliers/search", finH.SuppliersSearchAPI)

		auth.GET("/ledger", ledgerH.Cockpit)
		auth.GET("/ledger/settings", ledgerH.SettingsGet)
		auth.PUT("/ledger/settings", ledgerH.SettingsUpdate)
		auth.GET("/ledger/accounts", ledgerH.AccountsList)
		auth.POST("/ledger/accounts", ledgerH.AccountUpsert)
		auth.PUT("/ledger/accounts/:code", ledgerH.AccountUpsert)
		auth.GET("/ledger/periods", ledgerH.PeriodsList)
		auth.GET("/ledger/close-check", ledgerH.CloseCheck)
		auth.POST("/ledger/periods/:year/:month/close", ledgerH.PeriodClose)
		auth.POST("/ledger/periods/:year/:month/reopen", ledgerH.PeriodReopen)
		auth.GET("/ledger/vouchers", ledgerH.VouchersList)
		auth.POST("/ledger/vouchers", ledgerH.VoucherCreate)
		auth.GET("/ledger/vouchers/:id", ledgerH.VoucherGet)
		auth.PUT("/ledger/vouchers/:id", ledgerH.VoucherUpdate)
		auth.POST("/ledger/vouchers/:id/post", ledgerH.VoucherPost)
		auth.POST("/ledger/vouchers/:id/review", ledgerH.VoucherReview)
		auth.POST("/ledger/vouchers/:id/reject", ledgerH.VoucherReject)
		auth.POST("/ledger/vouchers/:id/reverse", ledgerH.VoucherReverse)
		auth.DELETE("/ledger/vouchers/:id", ledgerH.VoucherDelete)
		auth.GET("/ledger/books/trial", ledgerH.Trial)
		auth.GET("/ledger/books/general", ledgerH.Journal)
		auth.GET("/ledger/books/detail", ledgerH.Book)
		auth.GET("/ledger/books/cash", ledgerH.CashBook)
		auth.GET("/ledger/reports/balance-sheet", ledgerH.ReportBalanceSheet)
		auth.GET("/ledger/reports/income", ledgerH.ReportIncome)
		auth.GET("/ledger/reports/cashflow", ledgerH.ReportCashFlow)
		auth.POST("/ledger/openings", ledgerH.OpeningsSave)

		auth.GET("/personnel/departments", persH.DepartmentsPage)
		auth.POST("/personnel/departments", persH.DepartmentCreate)
		auth.PUT("/personnel/departments/:id", persH.DepartmentUpdate)
		auth.GET("/personnel/positions", persH.PositionsPage)
		auth.POST("/personnel/positions", persH.PositionCreate)
		auth.PUT("/personnel/positions/:id", persH.PositionUpdate)
		auth.DELETE("/personnel/positions/:id", persH.PositionDelete)
		auth.GET("/personnel/employees", persH.EmployeesPage)
		auth.POST("/personnel/employees", persH.EmployeeCreate)
		auth.GET("/personnel/employees/:id", persH.EmployeeDetailPage)
		auth.PUT("/personnel/employees/:id", persH.EmployeeUpdate)
		auth.POST("/personnel/employees/:id/deactivate", persH.EmployeeDeactivate)
		auth.GET("/personnel/contracts", persH.ContractsPage)
		auth.POST("/personnel/contracts", persH.ContractCreate)
		auth.GET("/personnel/salary", persH.SalaryListPage)
		auth.POST("/personnel/salary/batch", persH.SalaryBatchCreate)
		auth.GET("/personnel/salary/summary", persH.SalarySummaryAPI)
		auth.GET("/personnel/attendance-stats", persH.AttendanceStatsAPI)
		auth.GET("/personnel/salary/:id", persH.SalaryDetailPage)
		auth.POST("/personnel/salary/:id/confirm", persH.SalaryConfirm)

		auth.GET("/search", shared.GlobalSearchAPI(db))

		auth.GET("/picking", pickH.PickingOrdersPage)
		auth.POST("/picking/generate", pickH.GeneratePickingOrder)
		auth.GET("/picking/:id", pickH.PickingDetailPage)
		auth.POST("/picking/:id/items/:itemId", pickH.PickingItemUpdate)
		auth.POST("/picking/:id/complete", pickH.PickingComplete)
	}

	printed := r.Group("/")
	printed.Use(mw.AuthRequired(db))
	{
		printed.GET("/sales/:id/print", printH.SalesOrderPrint)
		printed.GET("/purchases/:id/print", printH.PurchaseOrderPrint)
		printed.GET("/finance/invoices/:id/print", printH.InvoicePrint)
		printed.GET("/finance/reconciliations/:id/print", printH.ReconciliationPrint)
		printed.GET("/finance/reimbursements/:id/print", printH.ReimbursementPrint)
		printed.GET("/customers/:id/statement/print", printH.CustomerStatement)
	}

	// 服务端渲染页面（templ + htmx）：与旧 SPA 同路径。
	web.New(db, companyName).RegisterHTML(r)

	// 未知路径：已登录回首页，未登录去登录页。
	r.NoRoute(func(c *gin.Context) {
		if mw.GetSession(c) != nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.Redirect(http.StatusFound, "/login")
	})
}
