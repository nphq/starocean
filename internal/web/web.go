package web

import (
	"database/sql"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	mw "github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/view/layout"
)

// Handler HTML 页面处理器（templ + htmx 服务端渲染）。
// JSON /api/* 保持不变（集成测试与外部集成继续可用）。
type Handler struct {
	db          *sql.DB
	companyName string
}

func New(db *sql.DB, companyName string) *Handler {
	return &Handler{db: db, companyName: companyName}
}

// HTMLAuth 未登录跳登录页（保留 next 回跳）；已登录放行。
func HTMLAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if mw.GetSession(c) == nil {
			next := c.Request.URL.RequestURI()
			c.Redirect(http.StatusFound, "/login?next="+next)
			c.Abort()
			return
		}
		c.Next()
	}
}

func isHX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

// redirect 303 回跳（表单 POST-Redirect-GET；htmx 场景用 HX-Redirect 则整页跳）。
func redirect(c *gin.Context, to string) {
	if isHX(c) {
		c.Header("HX-Redirect", to)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}

func (h *Handler) username(c *gin.Context) string {
	if s := mw.GetSession(c); s != nil {
		return s.Username
	}
	return ""
}

// renderPage 套 Base 布局渲染页面级组件。
func (h *Handler) renderPage(c *gin.Context, title string, content templ.Component) {
	ctx := c.Request.Context()
	page := layout.Base(title, c.Request.URL.Path, h.companyName, h.username(c), mw.GetRole(c))
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = page.Render(templ.WithChildren(ctx, content), c.Writer)
}

// renderFrag htmx 局部刷新：只渲染片段组件。
func renderFrag(c *gin.Context, content templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = content.Render(c.Request.Context(), c.Writer)
}

// RegisterHTML 注册服务端渲染页面路由（与旧 SPA 同路径，书签无缝迁移）。
func (h *Handler) RegisterHTML(r *gin.Engine) {
	r.GET("/login", h.LoginPage)
	r.POST("/login", h.LoginPost)
	r.GET("/logout", h.Logout)

	web := r.Group("/")
	web.Use(HTMLAuth())
	web.Use(mw.WriteRequired(h.db))
	{
		web.GET("/", h.Dashboard)

		web.GET("/products", h.ProductsPage)
		web.GET("/products/new", h.ProductNewPage)
		web.POST("/products", h.ProductCreate)
		web.GET("/products/:id", h.ProductDetail)
		web.GET("/products/:id/edit", h.ProductEditPage)
		web.POST("/products/:id", h.ProductUpdate)
		web.POST("/products/:id/delete", h.ProductDelete)
		web.POST("/products/:id/stock", h.ProductStockAdjust)

		web.GET("/customers", h.CustomersPage)
		web.GET("/customers/new", h.CustomerNewPage)
		web.POST("/customers", h.CustomerCreate)
		web.GET("/customers/:id", h.CustomerDetail)
		web.GET("/customers/:id/edit", h.CustomerEditPage)
		web.POST("/customers/:id", h.CustomerUpdate)
		web.POST("/customers/:id/delete", h.CustomerDelete)

		web.GET("/suppliers", h.SuppliersPage)
		web.GET("/suppliers/new", h.SupplierNewPage)
		web.POST("/suppliers", h.SupplierCreate)
		web.GET("/suppliers/:id", h.SupplierDetail)
		web.GET("/suppliers/:id/edit", h.SupplierEditPage)
		web.POST("/suppliers/:id", h.SupplierUpdate)
		web.POST("/suppliers/:id/delete", h.SupplierDelete)

		web.GET("/sales", h.SalesPage)
		web.GET("/sales/new", h.SaleNewPage)
		web.POST("/sales", h.SaleCreate)
		web.GET("/sales/:id", h.SaleDetail)
		web.POST("/sales/:id/items", h.SaleAddItem)
		web.POST("/sales/:id/confirm", h.SaleConfirm)
		web.POST("/sales/:id/ship", h.SaleShip)
		web.POST("/sales/:id/invoice", h.SaleInvoice)
		web.POST("/sales/:id/cancel", h.SaleCancel)
		web.POST("/sales/:id/delete", h.SaleDelete)

		web.GET("/purchases", h.PurchasesPage)
		web.GET("/purchases/new", h.PurchaseNewPage)
		web.POST("/purchases", h.PurchaseCreate)
		web.GET("/purchases/:id", h.PurchaseDetail)
		web.POST("/purchases/:id/items", h.PurchaseAddItem)
		web.POST("/purchases/:id/confirm", h.PurchaseConfirm)
		web.POST("/purchases/:id/receive", h.PurchaseReceive)
		web.POST("/purchases/:id/pay", h.PurchasePay)
		web.POST("/purchases/:id/cancel", h.PurchaseCancel)
		web.POST("/purchases/:id/delete", h.PurchaseDelete)

		web.GET("/inventory", h.InventoryPage)
		web.GET("/inventory/:productId", h.InventoryByProduct)

		web.GET("/finance", h.FinancePage)
		web.POST("/finance/payments", h.PaymentCreate)
		web.GET("/finance/reimbursements", h.ReimbursementsPage)
		web.GET("/finance/reimbursements/new", h.ReimbursementNewPage)
		web.POST("/finance/reimbursements", h.ReimbursementCreate)
		web.GET("/finance/reimbursements/:id", h.ReimbursementDetail)
		web.POST("/finance/reimbursements/:id/submit", h.ReimbursementSubmit)
		web.POST("/finance/reimbursements/:id/approve", h.ReimbursementApprove)
		web.POST("/finance/reimbursements/:id/pay", h.ReimbursementPay)
		web.GET("/finance/invoices", h.InvoicesPage)
		web.GET("/finance/invoices/new", h.InvoiceNewPage)
		web.POST("/finance/invoices", h.InvoiceCreate)
		web.GET("/finance/invoices/:id", h.InvoiceDetail)
		web.POST("/finance/invoices/:id/void", h.InvoiceVoid)
		web.GET("/finance/reconciliations", h.ReconciliationsPage)
		web.GET("/finance/reconciliations/:id", h.ReconciliationDetail)
		web.POST("/finance/reconciliations/:id/confirm", h.ReconciliationConfirm)

		web.GET("/ledger", h.LedgerCockpit)
		web.GET("/ledger/accounts", h.LedgerAccounts)
		web.GET("/ledger/vouchers", h.LedgerVouchers)
		web.GET("/ledger/vouchers/new", h.LedgerVoucherNew)
		web.POST("/ledger/vouchers", h.LedgerVoucherCreate)
		web.GET("/ledger/vouchers/:id", h.LedgerVoucherDetail)
		web.POST("/ledger/vouchers/:id/post", h.LedgerVoucherPost)
		web.POST("/ledger/vouchers/:id/review", h.LedgerVoucherReview)
		web.POST("/ledger/vouchers/:id/reject", h.LedgerVoucherReject)
		web.GET("/ledger/books", h.LedgerBooks)
		web.GET("/ledger/reports", h.LedgerReports)

		web.GET("/personnel/employees", h.EmployeesPage)
		web.GET("/personnel/employees/new", h.EmployeeNewPage)
		web.POST("/personnel/employees", h.EmployeeCreate)
		web.GET("/personnel/employees/:id", h.EmployeeDetail)
		web.GET("/personnel/salary", h.SalaryPage)
		web.GET("/personnel/contracts", h.ContractsPage)
		web.GET("/personnel/departments", h.DepartmentsPage)

		web.GET("/workreports", h.WorkReportsPage)
		web.GET("/workreports/new", h.WorkReportNewPage)
		web.POST("/workreports", h.WorkReportCreate)
		web.GET("/workreports/:id", h.WorkReportDetail)

		web.GET("/picking", h.PickingPage)
		web.GET("/picking/:id", h.PickingDetail)
		web.POST("/picking/:id/complete", h.PickingComplete)

		web.GET("/attendance", h.AttendancePage)

		web.GET("/search", h.SearchPage)
	}
}
