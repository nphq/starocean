package print

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	prt "github.com/nphq/starocean/view/print"
)

type Handler struct {
	db          *sql.DB
	companyName string
}

func New(db *sql.DB, companyName string) *Handler {
	return &Handler{db: db, companyName: companyName}
}

func (h *Handler) SalesOrderPrint(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	order, err := getSalesOrder(ctx, h.db, id)
	if err != nil {
		c.String(http.StatusNotFound, "订单不存在")
		return
	}

	items, err := getSalesOrderItems(ctx, h.db, id)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	var customer models.Customer
	if err := h.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(code,''), COALESCE(name,''),
			COALESCE(contact_person,''), COALESCE(phone,''),
			COALESCE(email,''), COALESCE(address,''),
			COALESCE(credit_limit,0), COALESCE(balance,0),
			COALESCE(created_at,'1970-01-01'),
			COALESCE(company_id,'default'), COALESCE(properties::text,'{}')
		FROM customers WHERE id = $1`, order.CustomerID).Scan(
		&customer.ID, &customer.Code, &customer.Name,
		&customer.ContactPerson, &customer.Phone,
		&customer.Email, &customer.Address,
		&customer.CreditLimit, &customer.Balance,
		&customer.CreatedAt, &customer.CompanyID, &customer.Properties); err != nil {
		c.String(http.StatusInternalServerError, "读取客户信息失败")
		return
	}

	// 打印响应直接写流，渲染错误无法再变更 HTTP 状态码。
	_ = prt.SalesOrderPrint(order, items, customer, h.companyName).Render(ctx, c.Writer)
}

func (h *Handler) PurchaseOrderPrint(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	order, err := getPurchaseOrder(ctx, h.db, id)
	if err != nil {
		c.String(http.StatusNotFound, "订单不存在")
		return
	}

	items, err := getPurchaseOrderItems(ctx, h.db, id)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	var supplier models.Supplier
	if err := h.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(code,''), COALESCE(name,''),
			COALESCE(contact_person,''), COALESCE(phone,''),
			COALESCE(email,''), COALESCE(address,''),
			COALESCE(balance,0), COALESCE(created_at,'1970-01-01'),
			COALESCE(company_id,'default'), COALESCE(properties::text,'{}')
		FROM suppliers WHERE id = $1`, order.SupplierID).Scan(
		&supplier.ID, &supplier.Code, &supplier.Name,
		&supplier.ContactPerson, &supplier.Phone,
		&supplier.Email, &supplier.Address,
		&supplier.Balance, &supplier.CreatedAt,
		&supplier.CompanyID, &supplier.Properties); err != nil {
		c.String(http.StatusInternalServerError, "读取供应商信息失败")
		return
	}

	_ = prt.PurchaseOrderPrint(order, items, supplier, h.companyName).Render(ctx, c.Writer)
}

func (h *Handler) InvoicePrint(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	var inv models.Invoice
	row := h.db.QueryRowContext(ctx, `SELECT id, invoice_no, type, partner_type, partner_id, COALESCE(partner_name,''),
		amount, tax_rate, tax_amount, total_amount,
		invoice_date, COALESCE(invoice_code,''), invoice_status,
		COALESCE(reference_type,''), reference_id,
		COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM invoices WHERE id = $1`, id)
	if err := row.Scan(&inv.ID, &inv.InvoiceNo, &inv.Type, &inv.PartnerType, &inv.PartnerID, &inv.PartnerName,
		&inv.Amount, &inv.TaxRate, &inv.TaxAmount, &inv.TotalAmount, &inv.InvoiceDate, &inv.InvoiceCode,
		&inv.InvoiceStatus, &inv.ReferenceType, &inv.ReferenceID, &inv.CreatedAt, &inv.CompanyID); err != nil {
		c.String(http.StatusNotFound, "发票不存在")
		return
	}

	_ = prt.InvoicePrint(inv, h.companyName).Render(ctx, c.Writer)
}

func (h *Handler) ReconciliationPrint(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	var r models.Reconciliation
	row := h.db.QueryRowContext(ctx, `SELECT id, reconciliation_no, partner_type, partner_id, COALESCE(partner_name,''),
		period_start, period_end, order_total, payment_total, discrepancy, status,
		COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM reconciliations WHERE id = $1`, id)
	if err := row.Scan(&r.ID, &r.ReconciliationNo, &r.PartnerType, &r.PartnerID, &r.PartnerName,
		&r.PeriodStart, &r.PeriodEnd, &r.OrderTotal, &r.PaymentTotal, &r.Discrepancy, &r.Status,
		&r.CreatedAt, &r.CompanyID); err != nil {
		c.String(http.StatusNotFound, "对账单不存在")
		return
	}

	orderItems, paymentItems := getReconciliationItems(ctx, h.db, id)

	_ = prt.ReconciliationPrint(r, orderItems, paymentItems, h.companyName).Render(ctx, c.Writer)
}

func (h *Handler) ReimbursementPrint(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	r, items, err := getReimbursement(ctx, h.db, id)
	if err != nil {
		c.String(http.StatusNotFound, "报销单不存在")
		return
	}

	_ = prt.ReimbursementPrint(*r, items, h.companyName).Render(ctx, c.Writer)
}

func (h *Handler) CustomerStatement(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}

	var customer models.Customer
	if err := h.db.QueryRowContext(ctx, `SELECT id, COALESCE(code,''), COALESCE(name,''),
		COALESCE(contact_person,''), COALESCE(phone,''),
		COALESCE(email,''), COALESCE(address,''),
		COALESCE(credit_limit,0), COALESCE(balance,0),
		COALESCE(created_at,'1970-01-01'),
		COALESCE(company_id,'default'), COALESCE(properties::text,'{}')
		FROM customers WHERE id = $1`, id).Scan(
		&customer.ID, &customer.Code, &customer.Name,
		&customer.ContactPerson, &customer.Phone,
		&customer.Email, &customer.Address,
		&customer.CreditLimit, &customer.Balance,
		&customer.CreatedAt, &customer.CompanyID, &customer.Properties); err != nil {
		c.String(http.StatusNotFound, "客户不存在")
		return
	}

	orderRows, err := h.db.QueryContext(ctx, `
		SELECT id, order_no, COALESCE(customer_id, gen_random_uuid()),
			COALESCE(status, 'draft'), COALESCE(total_amount, 0), COALESCE(paid_amount, 0),
			COALESCE(order_date, '1970-01-01'), COALESCE(delivery_date, '1970-01-01'),
			COALESCE(notes, ''), COALESCE(created_at, '1970-01-01'),
			COALESCE(company_id, 'default'), COALESCE(properties::text, '{}')
		FROM sales_orders WHERE customer_id = $1 AND status NOT IN ('cancelled','draft')
		ORDER BY order_date`, id)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer orderRows.Close()

	var orders []models.SalesOrder
	for orderRows.Next() {
		var o models.SalesOrder
		if err := orderRows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.Status,
			&o.TotalAmount, &o.PaidAmount, &o.OrderDate, &o.DeliveryDate,
			&o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err != nil {
			continue
		}
		orders = append(orders, o)
	}

	payments, _ := getCustomerPayments(ctx, h.db, customer.Name)

	_ = prt.CustomerStatementPrint(customer, orders, payments, h.companyName).Render(ctx, c.Writer)
}

func getSalesOrder(ctx context.Context, db *sql.DB, id uuid.UUID) (models.SalesOrder, error) {
	var o models.SalesOrder
	var deliveryDate sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT so.id, so.order_no, COALESCE(so.customer_id, gen_random_uuid()),
			COALESCE(c.name, '') as customer_name,
			COALESCE(so.status, 'draft'), COALESCE(so.total_amount, 0), COALESCE(so.paid_amount, 0),
			COALESCE(so.order_date, '1970-01-01'), so.delivery_date,
			COALESCE(so.notes, ''), COALESCE(so.created_at, '1970-01-01'),
			COALESCE(so.company_id, 'default'), COALESCE(so.properties::text, '{}')
		FROM sales_orders so LEFT JOIN customers c ON so.customer_id = c.id
		WHERE so.id = $1`, id).Scan(
		&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName,
		&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &deliveryDate,
		&o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties)
	if err != nil {
		return o, err
	}
	if deliveryDate.Valid {
		o.DeliveryDate = deliveryDate.Time
	}
	return o, nil
}

func getSalesOrderItems(ctx context.Context, db *sql.DB, orderID uuid.UUID) ([]models.SalesOrderItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT soi.id, COALESCE(soi.order_id, gen_random_uuid()),
			COALESCE(soi.product_id, gen_random_uuid()),
			COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
			soi.quantity, COALESCE(soi.unit_price, 0), COALESCE(soi.amount, 0)
		FROM sales_order_items soi
		LEFT JOIN products p ON soi.product_id = p.id
		WHERE soi.order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.SalesOrderItem
	for rows.Next() {
		var it models.SalesOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func getPurchaseOrder(ctx context.Context, db *sql.DB, id uuid.UUID) (models.PurchaseOrder, error) {
	var o models.PurchaseOrder
	var deliveryDate sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT po.id, po.order_no, COALESCE(po.supplier_id, gen_random_uuid()),
			COALESCE(s.name, '') as supplier_name,
			COALESCE(po.status, 'draft'), COALESCE(po.total_amount, 0), COALESCE(po.paid_amount, 0),
			COALESCE(po.order_date, '1970-01-01'), po.delivery_date,
			COALESCE(po.notes, ''), COALESCE(po.created_at, '1970-01-01'),
			COALESCE(po.company_id, 'default'), COALESCE(po.properties::text, '{}')
		FROM purchase_orders po LEFT JOIN suppliers s ON po.supplier_id = s.id
		WHERE po.id = $1`, id).Scan(
		&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName,
		&o.Status, &o.TotalAmount, &o.PaidAmount, &o.OrderDate, &deliveryDate,
		&o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties)
	if err != nil {
		return o, err
	}
	if deliveryDate.Valid {
		o.DeliveryDate = deliveryDate.Time
	}
	return o, nil
}

func getPurchaseOrderItems(ctx context.Context, db *sql.DB, orderID uuid.UUID) ([]models.PurchaseOrderItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT poi.id, COALESCE(poi.order_id, gen_random_uuid()),
			COALESCE(poi.product_id, gen_random_uuid()),
			COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
			poi.quantity, COALESCE(poi.unit_price, 0), COALESCE(poi.amount, 0)
		FROM purchase_order_items poi
		LEFT JOIN products p ON poi.product_id = p.id
		WHERE poi.order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.PurchaseOrderItem
	for rows.Next() {
		var it models.PurchaseOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func getReimbursement(ctx context.Context, db *sql.DB, id uuid.UUID) (*models.Reimbursement, []models.ReimbursementItem, error) {
	var r models.Reimbursement
	row := db.QueryRowContext(ctx, `SELECT id, reimbursement_no, applicant_name, COALESCE(department,''), amount, category, COALESCE(description,''),
		status, COALESCE(approver_name,''), approved_at, COALESCE(rejected_reason,''), payment_id, expense_date,
		COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM reimbursements WHERE id = $1`, id)
	if err := row.Scan(&r.ID, &r.ReimbursementNo, &r.ApplicantName, &r.Department, &r.Amount, &r.Category,
		&r.Description, &r.Status, &r.ApproverName, &r.ApprovedAt, &r.RejectedReason, &r.PaymentID,
		&r.ExpenseDate, &r.CreatedAt, &r.UpdatedAt, &r.CompanyID); err != nil {
		return nil, nil, err
	}

	itemRows, err := db.QueryContext(ctx, "SELECT id, reimbursement_id, category, amount, COALESCE(description,'') FROM reimbursement_items WHERE reimbursement_id=$1", id)
	if err != nil {
		return &r, nil, nil
	}
	defer itemRows.Close()
	var items []models.ReimbursementItem
	for itemRows.Next() {
		var item models.ReimbursementItem
		if err := itemRows.Scan(&item.ID, &item.ReimbursementID, &item.Category, &item.Amount, &item.Description); err != nil {
			continue
		}
		items = append(items, item)
	}
	return &r, items, nil
}

func getReconciliationItems(ctx context.Context, db *sql.DB, rID uuid.UUID) (orderItems, paymentItems []models.ReconciliationItem) {
	rows, err := db.QueryContext(ctx, `SELECT id, reconciliation_id, item_type, COALESCE(reference_no,''), amount, reference_date FROM reconciliation_items WHERE reconciliation_id=$1 ORDER BY item_type, reference_date`, rID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item models.ReconciliationItem
		var date *time.Time
		if rows.Scan(&item.ID, &item.ReconciliationID, &item.ItemType, &item.ReferenceNo, &item.Amount, &date) == nil {
			item.ReferenceDate = date
			if item.ItemType == "order" {
				orderItems = append(orderItems, item)
			} else {
				paymentItems = append(paymentItems, item)
			}
		}
	}
	return
}

func getCustomerPayments(ctx context.Context, db *sql.DB, customerName string) ([]models.Payment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, type, amount, COALESCE(partner_name,''),
			COALESCE(notes,''), COALESCE(payment_date,'1970-01-01'),
			COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM payments WHERE partner_name = $1 ORDER BY payment_date`, customerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []models.Payment
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.ID, &p.Type, &p.Amount, &p.PartnerName,
			&p.Notes, &p.PaymentDate, &p.CreatedAt, &p.CompanyID); err != nil {
			continue
		}
		payments = append(payments, p)
	}
	return payments, rows.Err()
}
