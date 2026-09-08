package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type DBTX interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

const companyID = "default"

type Settings struct {
	CashAccount      string `json:"cash_account"`
	BankAccount      string `json:"bank_account"`
	ARAccount        string `json:"ar_account"`
	APAccount        string `json:"ap_account"`
	InventoryAccount string `json:"inventory_account"`
	RevenueAccount   string `json:"revenue_account"`
	COGSAccount      string `json:"cogs_account"`
	OpexAccount      string `json:"opex_account"`
	PayrollAccount   string `json:"payroll_account"`
	IncomeSummary    string `json:"income_summary"`
	RetainedEarnings string `json:"retained_earnings"`
	SurplusAccount   string `json:"surplus_account"`
	AutoPost         bool   `json:"auto_post"`
	CostingMethod    string `json:"costing_method"`
}

type Account struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	ParentCode string `json:"parent_code"`
	Category   string `json:"category"`
	NormalSide string `json:"normal_side"`
	IsLeaf     bool   `json:"is_leaf"`
	IsCash     bool   `json:"is_cash"`
	AuxAR      bool   `json:"aux_ar"`
	AuxAP      bool   `json:"aux_ap"`
	Active     bool   `json:"active"`
	SortOrder  int    `json:"sort_order"`
}

type Period struct {
	Year      int        `json:"year"`
	Month     int        `json:"month"`
	StartDate string     `json:"start_date"`
	EndDate   string     `json:"end_date"`
	Status    string     `json:"status"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	ClosedBy  string     `json:"closed_by"`
}

type VoucherLine struct {
	ID          uuid.UUID       `json:"id"`
	LineNo      int             `json:"line_no"`
	AccountCode string          `json:"account_code"`
	AccountName string          `json:"account_name,omitempty"`
	Summary     string          `json:"summary"`
	Debit       decimal.Decimal `json:"debit"`
	Credit      decimal.Decimal `json:"credit"`
	PartnerType string          `json:"partner_type,omitempty"`
	PartnerID   *uuid.UUID      `json:"partner_id,omitempty"`
	PartnerName string          `json:"partner_name,omitempty"`
}

type Voucher struct {
	ID              uuid.UUID       `json:"id"`
	VoucherNo       string          `json:"voucher_no"`
	Word            string          `json:"word"`
	VoucherDate     string          `json:"voucher_date"`
	PeriodYear      int             `json:"period_year"`
	PeriodMonth     int             `json:"period_month"`
	AttachmentCount int             `json:"attachment_count"`
	Summary         string          `json:"summary"`
	Status          string          `json:"status"`
	SourceType      string          `json:"source_type"`
	SourceID        *uuid.UUID      `json:"source_id,omitempty"`
	PreparedBy      string          `json:"prepared_by"`
	PostedAt        *time.Time      `json:"posted_at,omitempty"`
	PostedBy        string          `json:"posted_by"`
	ReversesID      *uuid.UUID      `json:"reverses_id,omitempty"`
	ReversedByID    *uuid.UUID      `json:"reversed_by_id,omitempty"`
	DebitTotal      decimal.Decimal `json:"debit_total"`
	CreditTotal     decimal.Decimal `json:"credit_total"`
	CreatedAt       time.Time       `json:"created_at"`
	Lines           []VoucherLine   `json:"lines,omitempty"`
}

type LineInput struct {
	AccountCode string `json:"account_code"`
	Summary     string `json:"summary"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	PartnerType string `json:"partner_type"`
	PartnerID   string `json:"partner_id"`
	PartnerName string `json:"partner_name"`
}

type VoucherInput struct {
	Word            string      `json:"word"`
	VoucherDate     string      `json:"voucher_date"`
	AttachmentCount int         `json:"attachment_count"`
	Summary         string      `json:"summary"`
	SourceType      string      `json:"source_type"`
	SourceID        string      `json:"source_id"`
	Lines           []LineInput `json:"lines"`
}

func money(d decimal.Decimal) decimal.Decimal {
	return d.Round(2)
}

func parseMoney(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("金额无效: %s", s)
	}
	return money(v), nil
}

func loadSettings(ctx context.Context, db DBTX) (Settings, error) {
	var s Settings
	err := db.QueryRowContext(ctx, `
		SELECT cash_account, bank_account, ar_account, ap_account, inventory_account,
		       revenue_account, cogs_account, opex_account, payroll_account,
		       income_summary, retained_earnings, surplus_account, auto_post, costing_method
		FROM gl_settings WHERE company_id = $1`, companyID).Scan(
		&s.CashAccount, &s.BankAccount, &s.ARAccount, &s.APAccount, &s.InventoryAccount,
		&s.RevenueAccount, &s.COGSAccount, &s.OpexAccount, &s.PayrollAccount,
		&s.IncomeSummary, &s.RetainedEarnings, &s.SurplusAccount, &s.AutoPost, &s.CostingMethod,
	)
	if err != nil {
		return s, fmt.Errorf("读取总账设置: %w", err)
	}
	return s, nil
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("日期不能为空")
	}
	t, err := time.Parse("2006-01-02", s[:min(10, len(s))])
	if err != nil {
		return time.Time{}, fmt.Errorf("日期格式无效")
	}
	return t, nil
}

func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}

func periodOf(t time.Time) (int, int) {
	return t.Year(), int(t.Month())
}

func nextPeriod(year, month int) (int, int) {
	if month == 12 {
		return year + 1, 1
	}
	return year, month + 1
}

func prevPeriod(year, month int) (int, int) {
	if month == 1 {
		return year - 1, 12
	}
	return year, month - 1
}

func ending(openingDr, openingCr, periodDr, periodCr decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	net := openingDr.Sub(openingCr).Add(periodDr).Sub(periodCr)
	if net.GreaterThanOrEqual(decimal.Zero) {
		return money(net), decimal.Zero
	}
	return decimal.Zero, money(net.Neg())
}
