package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
	"github.com/shopspring/decimal"
)

// nextSeq 业务单号序列（与 finance.generateSeq 同模式：order_sequences 表）。
func (h *Handler) nextSeq(ctx context.Context, name, prefix string) string {
	var seq int64
	err := h.db.QueryRowContext(ctx, `
		INSERT INTO order_sequences (seq_key, last_seq) VALUES ($1, 1)
		ON CONFLICT (seq_key) DO UPDATE SET last_seq = order_sequences.last_seq + 1
		RETURNING last_seq`, "seq-"+name).Scan(&seq)
	if err != nil {
		return fmt.Sprintf("%s-%s-001", prefix, time.Now().Format("20060102"))
	}
	return fmt.Sprintf("%s-%s-%03d", prefix, time.Now().Format("20060102"), seq)
}

const reimbCols = `id, reimbursement_no, applicant_name, COALESCE(department,''), amount, category, COALESCE(description,''),
	status, COALESCE(approver_name,''), approved_at, COALESCE(rejected_reason,''), payment_id, expense_date,
	COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')`

func scanReimbRow(row interface{ Scan(...any) error }) (models.Reimbursement, error) {
	var r models.Reimbursement
	var approvedAt, expenseDate sql.NullTime
	var paymentID uuid.NullUUID
	err := row.Scan(&r.ID, &r.ReimbursementNo, &r.ApplicantName, &r.Department, &r.Amount, &r.Category,
		&r.Description, &r.Status, &r.ApproverName, &approvedAt, &r.RejectedReason, &paymentID,
		&expenseDate, &r.CreatedAt, &r.UpdatedAt, &r.CompanyID)
	if err != nil {
		return r, err
	}
	r.ApprovedAt = nullTime(approvedAt)
	r.PaymentID = nullUUID(paymentID)
	r.ExpenseDate = nullTime(expenseDate)
	return r, nil
}

func (h *Handler) ReimbursementsPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT `+reimbCols+` FROM reimbursements ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.Reimbursement
	for rows.Next() {
		if r, err := scanReimbRow(rows); err == nil {
			items = append(items, r)
		}
	}
	h.renderPage(c, "费用报销", pages.ReimbursementList(items))
}

func (h *Handler) ReimbursementNewPage(c *gin.Context) {
	h.renderPage(c, "新建报销单", pages.ReimbursementForm(""))
}

func (h *Handler) ReimbursementCreate(c *gin.Context) {
	ctx := c.Request.Context()
	applicant := strings.TrimSpace(c.PostForm("applicant_name"))
	amount := strings.TrimSpace(c.PostForm("amount"))
	fail := func(msg string) {
		h.renderPage(c, "新建报销单", pages.ReimbursementForm(msg))
	}
	if applicant == "" || amount == "" {
		fail("请填写申请人和金额")
		return
	}
	if _, err := shared.ParsePrice(amount); err != nil {
		fail("金额格式无效")
		return
	}
	no := h.nextSeq(ctx, "reimbursement_seq", "BX")
	var expenseDate any
	if d := strings.TrimSpace(c.PostForm("expense_date")); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			expenseDate = t
		}
	}
	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO reimbursements (id, reimbursement_no, applicant_name, department, amount, category, description, status, expense_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'draft',$8)`,
		id, no, applicant, strings.TrimSpace(c.PostForm("department")), amount,
		strings.TrimSpace(c.PostForm("category")), strings.TrimSpace(c.PostForm("description")), expenseDate); err != nil {
		fail("保存失败")
		return
	}
	redirect(c, "/finance/reimbursements/"+id.String())
}

func (h *Handler) ReimbursementDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	r, err := scanReimbRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+reimbCols+` FROM reimbursements WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "报销单不存在")
		return
	}
	h.renderPage(c, r.ReimbursementNo, pages.ReimbursementDetail(r, ""))
}

func (h *Handler) reimbAction(c *gin.Context, fn func(ctx context.Context, id uuid.UUID) error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	if err := fn(ctx, id); err != nil {
		r, rerr := scanReimbRow(h.db.QueryRowContext(ctx, `SELECT `+reimbCols+` FROM reimbursements WHERE id = $1`, id))
		if rerr != nil {
			c.String(http.StatusNotFound, "报销单不存在")
			return
		}
		msg := err.Error()
		if !isOrderBusinessErr(err) && !strings.Contains(msg, "状态") {
			msg = "操作失败，请稍后重试"
		}
		h.renderPage(c, r.ReimbursementNo, pages.ReimbursementDetail(r, msg))
		return
	}
	redirect(c, "/finance/reimbursements/"+id.String())
}

func (h *Handler) ReimbursementSubmit(c *gin.Context) {
	h.reimbAction(c, func(ctx context.Context, id uuid.UUID) error {
		res, err := h.db.ExecContext(ctx, "UPDATE reimbursements SET status='pending_approval', updated_at=NOW() WHERE id=$1 AND status='draft'", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("当前状态不可提交")
		}
		return nil
	})
}

func (h *Handler) ReimbursementApprove(c *gin.Context) {
	h.reimbAction(c, func(ctx context.Context, id uuid.UUID) error {
		res, err := h.db.ExecContext(ctx, "UPDATE reimbursements SET status='approved', approver_name=$1, approved_at=NOW(), updated_at=NOW() WHERE id=$2 AND status='pending_approval'", "web", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("当前状态不可审批")
		}
		return nil
	})
}

func (h *Handler) ReimbursementPay(c *gin.Context) {
	h.reimbAction(c, func(ctx context.Context, id uuid.UUID) error {
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var amount decimal.Decimal
		var applicantName, status, category string
		if err := tx.QueryRowContext(ctx, "SELECT amount, applicant_name, status, COALESCE(category,'') FROM reimbursements WHERE id=$1 FOR UPDATE", id).Scan(&amount, &applicantName, &status, &category); err != nil {
			return err
		}
		if status != "approved" {
			return fmt.Errorf("当前状态不可付款")
		}
		paymentID := uuid.New()
		if _, err := tx.ExecContext(ctx, `INSERT INTO payments (id, type, amount, partner_name, notes, payment_date)
			VALUES ($1, '支出', $2, $3, '报销付款', NOW())`, paymentID, amount.StringFixed(2), applicantName); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE reimbursements SET status='paid', payment_id=$1, updated_at=NOW() WHERE id=$2 AND status='approved'", paymentID, id); err != nil {
			return err
		}
		if err := ledger.PostReimbursement(ctx, tx, id, amount.StringFixed(2), category, applicantName, "web"); err != nil {
			return err
		}
		return tx.Commit()
	})
}

const invoiceCols = `id, invoice_no, type, partner_type, partner_id, COALESCE(partner_name,''), amount, tax_rate, tax_amount, total_amount,
	invoice_date, COALESCE(invoice_code,''), invoice_status, COALESCE(reference_type,''), reference_id, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')`

func scanInvoiceRow(row interface{ Scan(...any) error }) (models.Invoice, error) {
	var inv models.Invoice
	var partnerID, referenceID uuid.NullUUID
	var invDate sql.NullTime
	err := row.Scan(&inv.ID, &inv.InvoiceNo, &inv.Type, &inv.PartnerType, &partnerID, &inv.PartnerName,
		&inv.Amount, &inv.TaxRate, &inv.TaxAmount, &inv.TotalAmount, &invDate, &inv.InvoiceCode,
		&inv.InvoiceStatus, &inv.ReferenceType, &referenceID, &inv.CreatedAt, &inv.CompanyID)
	if err != nil {
		return inv, err
	}
	inv.PartnerID = nullUUID(partnerID)
	inv.InvoiceDate = nullTime(invDate)
	inv.ReferenceID = nullUUID(referenceID)
	return inv, nil
}

func (h *Handler) InvoicesPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT `+invoiceCols+` FROM invoices ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.Invoice
	for rows.Next() {
		if inv, err := scanInvoiceRow(rows); err == nil {
			items = append(items, inv)
		}
	}
	h.renderPage(c, "发票管理", pages.InvoiceList(items))
}

func (h *Handler) InvoiceNewPage(c *gin.Context) {
	h.renderPage(c, "新建发票", pages.InvoiceForm(""))
}

func (h *Handler) InvoiceCreate(c *gin.Context) {
	ctx := c.Request.Context()
	fail := func(msg string) {
		h.renderPage(c, "新建发票", pages.InvoiceForm(msg))
	}
	invNo := strings.TrimSpace(c.PostForm("invoice_no"))
	amount := strings.TrimSpace(c.PostForm("amount"))
	if invNo == "" {
		fail("请输入发票号码")
		return
	}
	if _, err := shared.ParsePrice(amount); err != nil {
		fail("金额格式无效")
		return
	}
	taxRate := strings.TrimSpace(c.PostForm("tax_rate"))
	if taxRate == "" {
		taxRate = "0"
	}
	amt, _ := decimal.NewFromString(amount)
	rate, _ := decimal.NewFromString(taxRate)
	taxAmt := amt.Mul(rate).Round(2)
	var invDate any
	if d := strings.TrimSpace(c.PostForm("invoice_date")); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			invDate = t
		}
	}
	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO invoices (id, invoice_no, type, partner_type, partner_id, partner_name, amount, tax_rate, tax_amount, invoice_code, invoice_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, invNo, c.PostForm("type"), c.PostForm("partner_type"), nil,
		strings.TrimSpace(c.PostForm("partner_name")), amount, taxRate, taxAmt.StringFixed(2),
		strings.TrimSpace(c.PostForm("invoice_code")), invDate); err != nil {
		fail("保存失败")
		return
	}
	redirect(c, "/finance/invoices/"+id.String())
}

func (h *Handler) InvoiceDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	inv, err := scanInvoiceRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+invoiceCols+` FROM invoices WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "发票不存在")
		return
	}
	h.renderPage(c, inv.InvoiceNo, pages.InvoiceDetail(inv, ""))
}

func (h *Handler) InvoiceVoid(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), "UPDATE invoices SET invoice_status='void' WHERE id=$1 AND invoice_status='normal'", id)
	redirect(c, "/finance/invoices/"+id.String())
}

const reconCols = `id, reconciliation_no, partner_type, partner_id, COALESCE(partner_name,''),
	period_start, period_end, order_total, payment_total, discrepancy, status, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')`

func (h *Handler) ReconciliationsPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT `+reconCols+` FROM reconciliations ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.Reconciliation
	for rows.Next() {
		var r models.Reconciliation
		if err := rows.Scan(&r.ID, &r.ReconciliationNo, &r.PartnerType, &r.PartnerID, &r.PartnerName,
			&r.PeriodStart, &r.PeriodEnd, &r.OrderTotal, &r.PaymentTotal, &r.Discrepancy, &r.Status, &r.CreatedAt, &r.CompanyID); err == nil {
			items = append(items, r)
		}
	}
	h.renderPage(c, "对账管理", pages.ReconciliationList(items))
}

func (h *Handler) ReconciliationDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	var r models.Reconciliation
	err = h.db.QueryRowContext(c.Request.Context(), `SELECT `+reconCols+` FROM reconciliations WHERE id = $1`, id).Scan(
		&r.ID, &r.ReconciliationNo, &r.PartnerType, &r.PartnerID, &r.PartnerName,
		&r.PeriodStart, &r.PeriodEnd, &r.OrderTotal, &r.PaymentTotal, &r.Discrepancy, &r.Status, &r.CreatedAt, &r.CompanyID)
	if err != nil {
		c.String(http.StatusNotFound, "对账单不存在")
		return
	}
	h.renderPage(c, r.ReconciliationNo, pages.ReconciliationDetail(r))
}

func (h *Handler) ReconciliationConfirm(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), "UPDATE reconciliations SET status='confirmed' WHERE id=$1 AND status='draft'", id)
	redirect(c, "/finance/reconciliations/"+id.String())
}
