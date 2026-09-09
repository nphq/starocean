package finance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/shopspring/decimal"
)

type reimbursementItemInput struct {
	Category    string `json:"category"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
}

type reimbursementInput struct {
	ApplicantName string                   `json:"applicant_name"`
	Department    string                   `json:"department"`
	Amount        string                   `json:"amount"`
	Category      string                   `json:"category"`
	Description   string                   `json:"description"`
	ExpenseDate   string                   `json:"expense_date"`
	Action        string                   `json:"action"`
	Items         []reimbursementItemInput `json:"items"`
}

type rejectInput struct {
	Reason string `json:"reason"`
}

type approveInput struct {
	ApproverName string `json:"approver_name"`
}

type invoiceInput struct {
	InvoiceNo   string    `json:"invoice_no"`
	Type        string    `json:"type"`
	PartnerType string    `json:"partner_type"`
	PartnerID   uuid.UUID `json:"partner_id"`
	PartnerName string    `json:"partner_name"`
	Amount      string    `json:"amount"`
	TaxRate     string    `json:"tax_rate"`
	TaxAmount   string    `json:"tax_amount"`
	InvoiceCode string    `json:"invoice_code"`
	InvoiceDate string    `json:"invoice_date"`
}

type reconciliationInput struct {
	PartnerType string    `json:"partner_type"`
	PartnerID   uuid.UUID `json:"partner_id"`
	PartnerName string    `json:"partner_name"`
	PeriodStart string    `json:"period_start"`
	PeriodEnd   string    `json:"period_end"`
}

type reimbursementDetail struct {
	models.Reimbursement
	Items []models.ReimbursementItem `json:"items"`
}

type reconciliationDetail struct {
	models.Reconciliation
	OrderItems   []models.ReconciliationItem `json:"order_items"`
	PaymentItems []models.ReconciliationItem `json:"payment_items"`
}

func parseOptionalDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &d
}

func (h *Handler) ReimbursementsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	tab := c.DefaultQuery("tab", "all")

	where := "1=1"
	args := []interface{}{}
	argIdx := 1

	if tab != "all" {
		where = fmt.Sprintf("status = $%d", argIdx)
		args = append(args, tab)
		argIdx++
	}

	queryArgs := append(args, 20, (page-1)*20)
	rows, err := h.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, reimbursement_no, applicant_name, COALESCE(department,''), amount, category, COALESCE(description,''),
			status, COALESCE(approver_name,''), approved_at, COALESCE(rejected_reason,''), payment_id, expense_date,
			COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')
			FROM reimbursements WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1),
		queryArgs...)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var items []models.Reimbursement
	for rows.Next() {
		var r models.Reimbursement
		if err := rows.Scan(&r.ID, &r.ReimbursementNo, &r.ApplicantName, &r.Department, &r.Amount, &r.Category,
			&r.Description, &r.Status, &r.ApproverName, &r.ApprovedAt, &r.RejectedReason, &r.PaymentID,
			&r.ExpenseDate, &r.CreatedAt, &r.UpdatedAt, &r.CompanyID); err != nil {
			continue
		}
		items = append(items, r)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM reimbursements WHERE %s", where), args...).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Reimbursement]{Items: shared.EmptySlice(items), Total: count, Page: page})
}

func (h *Handler) ReimbursementCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in reimbursementInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.ApplicantName == "" || in.Amount == "" {
		shared.JSONBadRequest(c, "请填写申请人和金额")
		return
	}

	no, _ := generateSeq(ctx, h.db, "reimbursement_seq", "BX")
	status := "draft"
	if in.Action == "submit" {
		status = "pending_approval"
	}

	expenseDate := parseOptionalDate(in.ExpenseDate)

	rID := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO reimbursements (id, reimbursement_no, applicant_name, department, amount, category, description, status, expense_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		rID, no, in.ApplicantName, in.Department, in.Amount, in.Category,
		in.Description, status, expenseDate)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	for _, item := range in.Items {
		amt := item.Amount
		if amt == "" {
			amt = "0"
		}
		if _, err := h.db.ExecContext(ctx, `INSERT INTO reimbursement_items (id, reimbursement_id, category, amount, description) VALUES ($1,$2,$3,$4,$5)`,
			uuid.New(), rID, item.Category, amt, item.Description); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": rID})
}

func (h *Handler) ReimbursementDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	r, items, err := getReimbursement(ctx, h.db, id)
	if err != nil {
		shared.JSONNotFound(c, "报销单不存在")
		return
	}

	shared.JSONOK(c, reimbursementDetail{Reimbursement: *r, Items: shared.EmptySlice(items)})
}

func (h *Handler) ReimbursementSubmit(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE reimbursements SET status='pending_approval', updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$1 AND status='draft'", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ReimbursementApprove(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in approveInput
	_ = c.ShouldBindJSON(&in)
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "reimbursement.approving", map[string]interface{}{"id": id.String()})) {
		return
	}
	approver := in.ApproverName
	now := time.Now()
	res, err := h.db.ExecContext(ctx, "UPDATE reimbursements SET status='approved', approver_name=$1, approved_at=$2, updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$3 AND status='pending_approval'", approver, now, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		shared.FireAfter("reimbursement.approved", map[string]interface{}{"id": id.String()})
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ReimbursementReject(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in rejectInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE reimbursements SET status='rejected', rejected_reason=$1, updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$2 AND status='pending_approval'", in.Reason, id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ReimbursementPay(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	var amount decimal.Decimal
	var applicantName, status, category string
	err = tx.QueryRowContext(ctx, "SELECT amount, applicant_name, status, COALESCE(category,'') FROM reimbursements WHERE id=$1", id).Scan(&amount, &applicantName, &status, &category)
	if err != nil {
		shared.JSONNotFound(c, "报销单不存在")
		return
	}
	if status != "approved" {
		shared.JSONBadRequest(c, "当前状态不可付款")
		return
	}

	paymentID := uuid.New()
	_, err = tx.ExecContext(ctx, `INSERT INTO payments (id, type, amount, partner_name, notes, payment_date)
		VALUES ($1, '支出', $2, $3, '报销付款', (strftime('%Y-%m-%dT%H:%M:%SZ','now')))`, paymentID, amount.StringFixed(2), applicantName)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	res, err := tx.ExecContext(ctx, "UPDATE reimbursements SET status='paid', payment_id=$1, updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$2 AND status='approved'", paymentID, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		shared.JSONBadRequest(c, "付款失败")
		return
	}
	if err := ledger.PostReimbursement(ctx, tx, id, amount.StringFixed(2), category, applicantName, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("payment.created", map[string]interface{}{"type": "支出", "amount": amount.StringFixed(2)})
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) InvoicesPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	tab := c.DefaultQuery("tab", "all")

	where := "1=1"
	args := []interface{}{}
	argIdx := 1

	if tab == "input" || tab == "output" {
		where = fmt.Sprintf("type = $%d", argIdx)
		args = append(args, tab)
		argIdx++
	}

	queryArgs := append(args, 20, (page-1)*20)
	rows, err := h.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, invoice_no, type, partner_type, partner_id, COALESCE(partner_name,''), amount, tax_rate, tax_amount, total_amount,
			invoice_date, COALESCE(invoice_code,''), invoice_status, COALESCE(reference_type,''), reference_id, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
			FROM invoices WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1),
		queryArgs...)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var items []models.Invoice
	for rows.Next() {
		var inv models.Invoice
		if err := rows.Scan(&inv.ID, &inv.InvoiceNo, &inv.Type, &inv.PartnerType, &inv.PartnerID, &inv.PartnerName,
			&inv.Amount, &inv.TaxRate, &inv.TaxAmount, &inv.TotalAmount, &inv.InvoiceDate, &inv.InvoiceCode,
			&inv.InvoiceStatus, &inv.ReferenceType, &inv.ReferenceID, &inv.CreatedAt, &inv.CompanyID); err != nil {
			continue
		}
		items = append(items, inv)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM invoices WHERE %s", where), args...).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Invoice]{Items: shared.EmptySlice(items), Total: count, Page: page})
}

func (h *Handler) InvoiceCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in invoiceInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.InvoiceNo == "" {
		shared.JSONBadRequest(c, "请输入发票号码")
		return
	}

	invoiceDate := parseOptionalDate(in.InvoiceDate)

	var partnerID *uuid.UUID
	if in.PartnerID != uuid.Nil {
		pid := in.PartnerID
		partnerID = &pid
	}

	id := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO invoices (id, invoice_no, type, partner_type, partner_id, partner_name, amount, tax_rate, tax_amount, invoice_code, invoice_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, in.InvoiceNo, in.Type, in.PartnerType, partnerID, in.PartnerName,
		in.Amount, in.TaxRate, in.TaxAmount, in.InvoiceCode, invoiceDate)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) InvoiceDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	row := h.db.QueryRowContext(ctx, `SELECT id, invoice_no, type, partner_type, partner_id, COALESCE(partner_name,''), amount, tax_rate, tax_amount, total_amount,
		invoice_date, COALESCE(invoice_code,''), invoice_status, COALESCE(reference_type,''), reference_id, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM invoices WHERE id = $1`, id)

	var inv models.Invoice
	if err := row.Scan(&inv.ID, &inv.InvoiceNo, &inv.Type, &inv.PartnerType, &inv.PartnerID, &inv.PartnerName,
		&inv.Amount, &inv.TaxRate, &inv.TaxAmount, &inv.TotalAmount, &inv.InvoiceDate, &inv.InvoiceCode,
		&inv.InvoiceStatus, &inv.ReferenceType, &inv.ReferenceID, &inv.CreatedAt, &inv.CompanyID); err != nil {
		shared.JSONNotFound(c, "发票不存在")
		return
	}

	shared.JSONOK(c, inv)
}

func (h *Handler) InvoiceVoid(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE invoices SET invoice_status='void' WHERE id=$1 AND invoice_status='normal'", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ReconciliationsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)

	rows, err := h.db.QueryContext(ctx, `SELECT id, reconciliation_no, partner_type, partner_id, COALESCE(partner_name,''),
		period_start, period_end, order_total, payment_total, discrepancy, status, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM reconciliations ORDER BY created_at DESC LIMIT $1 OFFSET $2`, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var items []models.Reconciliation
	for rows.Next() {
		var r models.Reconciliation
		if err := rows.Scan(&r.ID, &r.ReconciliationNo, &r.PartnerType, &r.PartnerID, &r.PartnerName,
			&r.PeriodStart, &r.PeriodEnd, &r.OrderTotal, &r.PaymentTotal, &r.Discrepancy, &r.Status, &r.CreatedAt, &r.CompanyID); err != nil {
			continue
		}
		items = append(items, r)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reconciliations").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Reconciliation]{Items: shared.EmptySlice(items), Total: count, Page: page})
}

func (h *Handler) ReconciliationCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in reconciliationInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.PartnerType == "" || in.PartnerID == uuid.Nil {
		shared.JSONBadRequest(c, "请选择往来方和日期")
		return
	}

	periodStart, _ := time.Parse("2006-01-02", in.PeriodStart)
	periodEnd, _ := time.Parse("2006-01-02", in.PeriodEnd)

	no, _ := generateSeq(ctx, h.db, "reconciliation_seq", "DZ")
	orderTotal, paymentTotal := calculateReconciliationTotals(ctx, h.db, in.PartnerType, in.PartnerID, periodStart, periodEnd)
	discrepancy := orderTotal.Sub(paymentTotal)

	rID := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO reconciliations (id, reconciliation_no, partner_type, partner_id, partner_name, period_start, period_end, order_total, payment_total, discrepancy, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft')`,
		rID, no, in.PartnerType, in.PartnerID, in.PartnerName, periodStart, periodEnd, orderTotal, paymentTotal, discrepancy)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	insertReconciliationItems(ctx, h.db, rID, in.PartnerType, in.PartnerID, periodStart, periodEnd)

	shared.JSONCreated(c, gin.H{"ok": true, "id": rID})
}

func (h *Handler) ReconciliationDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	row := h.db.QueryRowContext(ctx, `SELECT id, reconciliation_no, partner_type, partner_id, COALESCE(partner_name,''),
		period_start, period_end, order_total, payment_total, discrepancy, status, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM reconciliations WHERE id = $1`, id)

	var r models.Reconciliation
	if err := row.Scan(&r.ID, &r.ReconciliationNo, &r.PartnerType, &r.PartnerID, &r.PartnerName,
		&r.PeriodStart, &r.PeriodEnd, &r.OrderTotal, &r.PaymentTotal, &r.Discrepancy, &r.Status, &r.CreatedAt, &r.CompanyID); err != nil {
		shared.JSONNotFound(c, "对账单不存在")
		return
	}

	orderItems, paymentItems := getReconciliationItems(ctx, h.db, id)
	shared.JSONOK(c, reconciliationDetail{
		Reconciliation: r,
		OrderItems:     shared.EmptySlice(orderItems),
		PaymentItems:   shared.EmptySlice(paymentItems),
	})
}

func (h *Handler) ReconciliationConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE reconciliations SET status='confirmed' WHERE id=$1 AND status='draft'", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ReimbursementUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in reimbursementInput
	if !shared.BindJSON(c, &in) {
		return
	}

	expenseDate := parseOptionalDate(in.ExpenseDate)

	_, err = h.db.ExecContext(ctx, `UPDATE reimbursements SET applicant_name=$1, department=$2, amount=$3, category=$4, description=$5, expense_date=$6, updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$7`,
		in.ApplicantName, in.Department, in.Amount, in.Category, in.Description, expenseDate, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	if _, err := h.db.ExecContext(ctx, "DELETE FROM reimbursement_items WHERE reimbursement_id=$1", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	for _, item := range in.Items {
		amt := item.Amount
		if amt == "" {
			amt = "0"
		}
		if _, err := h.db.ExecContext(ctx, `INSERT INTO reimbursement_items (id, reimbursement_id, category, amount, description) VALUES ($1,$2,$3,$4,$5)`,
			uuid.New(), id, item.Category, amt, item.Description); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func generateSeq(ctx context.Context, db *sql.DB, seqName, prefix string) (string, error) {
	// 用 order_sequences 表实现自增序列（与订单号生成同一模式）。
	var seq int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO order_sequences (seq_key, last_seq) VALUES ($1, 1)
		ON CONFLICT (seq_key) DO UPDATE SET last_seq = order_sequences.last_seq + 1
		RETURNING last_seq`, "seq-"+seqName).Scan(&seq)
	if err != nil {
		return "", err
	}
	now := time.Now().Format("20060102")
	return fmt.Sprintf("%s-%s-%03d", prefix, now, seq), nil
}

func getReimbursement(ctx context.Context, db *sql.DB, id uuid.UUID) (*models.Reimbursement, []models.ReimbursementItem, error) {
	row := db.QueryRowContext(ctx, `SELECT id, reimbursement_no, applicant_name, COALESCE(department,''), amount, category, COALESCE(description,''),
		status, COALESCE(approver_name,''), approved_at, COALESCE(rejected_reason,''), payment_id, expense_date,
		COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM reimbursements WHERE id = $1`, id)

	var r models.Reimbursement
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

func calculateReconciliationTotals(ctx context.Context, db *sql.DB, partnerType string, partnerID uuid.UUID, start, end time.Time) (decimal.Decimal, decimal.Decimal) {
	var orderTotal, paymentTotal decimal.Decimal

	if partnerType == "customer" {
		// 汇总为尽力而为：查询失败保持零值，不向上传播。
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_amount),0) FROM sales_orders WHERE customer_id=$1 AND order_date >= $2 AND order_date <= $3 AND status != 'cancelled'`,
			partnerID, start, end).Scan(&orderTotal)
		var partnerName string
		_ = db.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM customers WHERE id=$1", partnerID).Scan(&partnerName)
		// 按 partner_id 精确关联；历史无 partner_id 的付款降级按名匹配。
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(amount AS numeric)),0) FROM payments WHERE payment_date >= $1 AND payment_date <= $2 AND ((partner_id = $3 AND partner_type = $4) OR (partner_id IS NULL AND partner_name = $5))`,
			start, end, partnerID, partnerType, partnerName).Scan(&paymentTotal)
	} else {
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_amount),0) FROM purchase_orders WHERE supplier_id=$1 AND order_date >= $2 AND order_date <= $3 AND status != 'cancelled'`,
			partnerID, start, end).Scan(&orderTotal)
		var partnerName string
		_ = db.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM suppliers WHERE id=$1", partnerID).Scan(&partnerName)
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(amount AS numeric)),0) FROM payments WHERE payment_date >= $1 AND payment_date <= $2 AND ((partner_id = $3 AND partner_type = $4) OR (partner_id IS NULL AND partner_name = $5))`,
			start, end, partnerID, partnerType, partnerName).Scan(&paymentTotal)
	}

	return orderTotal, paymentTotal
}

func insertReconciliationItems(ctx context.Context, db *sql.DB, rID uuid.UUID, partnerType string, partnerID uuid.UUID, start, end time.Time) {
	if partnerType == "customer" {
		rows, err := db.QueryContext(ctx, `SELECT order_no, total_amount, order_date FROM sales_orders WHERE customer_id=$1 AND order_date >= $2 AND order_date <= $3 AND status != 'cancelled'`,
			partnerID, start, end)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var orderNo string
				var amount decimal.Decimal
				var date time.Time
				if rows.Scan(&orderNo, &amount, &date) == nil {
					if _, err := db.ExecContext(ctx, `INSERT INTO reconciliation_items (id, reconciliation_id, item_type, reference_no, amount, reference_date) VALUES ($1,$2,'order',$3,$4,$5)`,
						uuid.New(), rID, orderNo, amount, date); err != nil {
						continue
					}
				}
			}
		}
	} else {
		rows, err := db.QueryContext(ctx, `SELECT order_no, total_amount, order_date FROM purchase_orders WHERE supplier_id=$1 AND order_date >= $2 AND order_date <= $3 AND status != 'cancelled'`,
			partnerID, start, end)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var orderNo string
				var amount decimal.Decimal
				var date time.Time
				if rows.Scan(&orderNo, &amount, &date) == nil {
					if _, err := db.ExecContext(ctx, `INSERT INTO reconciliation_items (id, reconciliation_id, item_type, reference_no, amount, reference_date) VALUES ($1,$2,'order',$3,$4,$5)`,
						uuid.New(), rID, orderNo, amount, date); err != nil {
						continue
					}
				}
			}
		}
	}

	var partnerName string
	if partnerType == "customer" {
		_ = db.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM customers WHERE id=$1", partnerID).Scan(&partnerName)
	} else {
		_ = db.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM suppliers WHERE id=$1", partnerID).Scan(&partnerName)
	}

	paymentRows, err := db.QueryContext(ctx, `SELECT COALESCE(notes,''), amount, payment_date FROM payments
		WHERE payment_date >= $1 AND payment_date <= $2
		  AND ((partner_id = $3 AND partner_type = $4) OR (partner_id IS NULL AND partner_name = $5))
		ORDER BY payment_date`,
		start, end, partnerID, partnerType, partnerName)
	if err == nil {
		defer paymentRows.Close()
		for paymentRows.Next() {
			var notes string
			var amount decimal.Decimal
			var pdate time.Time
			if paymentRows.Scan(&notes, &amount, &pdate) == nil {
				if _, err := db.ExecContext(ctx, `INSERT INTO reconciliation_items (id, reconciliation_id, item_type, reference_no, amount, reference_date) VALUES ($1,$2,'payment',$3,$4,$5)`,
					uuid.New(), rID, notes, amount, pdate); err != nil {
					continue
				}
			}
		}
	}
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

func (h *Handler) ReconciliationPreview(c *gin.Context) {
	ctx := c.Request.Context()

	partnerType := c.Query("partner_type")
	partnerIDStr := c.Query("partner_id")
	startStr := c.Query("period_start")
	endStr := c.Query("period_end")

	partnerID, _ := uuid.Parse(partnerIDStr)
	start, _ := time.Parse("2006-01-02", startStr)
	end, _ := time.Parse("2006-01-02", endStr)

	orderTotal, paymentTotal := calculateReconciliationTotals(ctx, h.db, partnerType, partnerID, start, end)

	shared.JSONOK(c, gin.H{
		"order_total":   orderTotal.StringFixed(2),
		"payment_total": paymentTotal.StringFixed(2),
		"discrepancy":   orderTotal.Sub(paymentTotal).StringFixed(2),
	})
}

func (h *Handler) CustomersSearchAPI(c *gin.Context) {
	ctx := c.Request.Context()
	q := c.Query("q")

	rows, err := h.db.QueryContext(ctx, "SELECT id, code, name FROM customers WHERE name LIKE $1 ORDER BY name LIMIT 20", "%"+q+"%")
	if err != nil {
		shared.JSONOK(c, []interface{}{})
		return
	}
	defer rows.Close()
	var items []gin.H
	for rows.Next() {
		var id uuid.UUID
		var code, name string
		if err := rows.Scan(&id, &code, &name); err != nil {
			continue
		}
		items = append(items, gin.H{"id": id.String(), "code": code, "name": name})
	}
	shared.JSONOK(c, shared.EmptySlice(items))
}

func (h *Handler) SuppliersSearchAPI(c *gin.Context) {
	ctx := c.Request.Context()
	q := c.Query("q")

	rows, err := h.db.QueryContext(ctx, "SELECT id, code, name FROM suppliers WHERE name LIKE $1 ORDER BY name LIMIT 20", "%"+q+"%")
	if err != nil {
		shared.JSONOK(c, []interface{}{})
		return
	}
	defer rows.Close()
	var items []gin.H
	for rows.Next() {
		var id uuid.UUID
		var code, name string
		if err := rows.Scan(&id, &code, &name); err != nil {
			continue
		}
		items = append(items, gin.H{"id": id.String(), "code": code, "name": name})
	}
	shared.JSONOK(c, shared.EmptySlice(items))
}
