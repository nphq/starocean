package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CloseBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
	Href    string `json:"href,omitempty"`
}

type CloseCheck struct {
	Year            int            `json:"year"`
	Month           int            `json:"month"`
	Status          string         `json:"status"`
	CanClose        bool           `json:"can_close"`
	CanReopen       bool           `json:"can_reopen"`
	DraftCount      int64          `json:"draft_count"`
	ReviewedCount   int64          `json:"reviewed_count"`
	PostedCount     int64          `json:"posted_count"`
	Unbalanced      int64          `json:"unbalanced"`
	TrialBalanced   bool           `json:"trial_balanced"`
	TrialDebit      string         `json:"trial_debit"`
	TrialCredit     string         `json:"trial_credit"`
	IncomeNet       string         `json:"income_net"`
	Blockers        []CloseBlocker `json:"blockers"`
	HasCloseVoucher bool           `json:"has_close_voucher"`
}

func CheckClose(ctx context.Context, db DBTX, year, month int) (CloseCheck, error) {
	p, err := ensurePeriod(ctx, db, year, month)
	if err != nil {
		return CloseCheck{}, err
	}
	out := CloseCheck{Year: year, Month: month, Status: p.Status, Blockers: []CloseBlocker{}}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE status='draft'),
		       COUNT(*) FILTER (WHERE status='reviewed'),
		       COUNT(*) FILTER (WHERE status='posted'),
		       COUNT(*) FILTER (WHERE status IN ('draft','reviewed') AND debit_total <> credit_total)
		FROM gl_vouchers WHERE period_year=$1 AND period_month=$2 AND company_id=$3`,
		year, month, companyID).Scan(&out.DraftCount, &out.ReviewedCount, &out.PostedCount, &out.Unbalanced); err != nil {
		return out, err
	}

	tb, err := TrialBalance(ctx, db, year, month, false)
	if err != nil {
		return out, err
	}
	out.TrialDebit = tb.DebitTotal
	out.TrialCredit = tb.CreditTotal
	out.TrialBalanced = tb.Balanced
	out.IncomeNet = tb.IncomeNet

	var closeCount int64
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gl_vouchers
		WHERE source_type='period_close' AND period_year=$1 AND period_month=$2 AND status='posted' AND reversed_by_id IS NULL`,
		year, month).Scan(&closeCount)
	out.HasCloseVoucher = closeCount > 0

	if p.Status == "closed" {
		ny, nm := nextPeriod(year, month)
		var nextPosted int64
		_ = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM gl_vouchers WHERE period_year=$1 AND period_month=$2 AND status='posted' AND source_type <> 'period_close'`,
			ny, nm).Scan(&nextPosted)
		out.CanReopen = nextPosted == 0
		if !out.CanReopen {
			out.Blockers = append(out.Blockers, CloseBlocker{
				Code: "next_posted", Message: "下一期间已有过账凭证，不能反结账", Count: nextPosted,
			})
		}
		return out, nil
	}

	py, pm := prevPeriod(year, month)
	if prev, err := getPeriod(ctx, db, py, pm); err == nil && prev.Status != "closed" {
		out.Blockers = append(out.Blockers, CloseBlocker{
			Code:    "prior_open",
			Message: fmt.Sprintf("%d年%02d月尚未结账，不能跳月", py, pm),
			Href:    fmt.Sprintf("/ledger?year=%d&month=%d", py, pm),
		})
	}

	if unposted := out.DraftCount + out.ReviewedCount; unposted > 0 {
		out.Blockers = append(out.Blockers, CloseBlocker{
			Code: "drafts", Message: "存在未过账凭证", Count: unposted,
			Href: fmt.Sprintf("/ledger/vouchers?year=%d&month=%d&status=draft", year, month),
		})
	}
	if out.Unbalanced > 0 {
		out.Blockers = append(out.Blockers, CloseBlocker{
			Code: "unbalanced", Message: "存在借贷不平的草稿", Count: out.Unbalanced,
		})
	}
	if !out.TrialBalanced {
		out.Blockers = append(out.Blockers, CloseBlocker{
			Code: "trial", Message: fmt.Sprintf("科目余额表不平衡（借 %s / 贷 %s）", out.TrialDebit, out.TrialCredit),
		})
	}
	out.CanClose = len(out.Blockers) == 0
	return out, nil
}

func ClosePeriod(ctx context.Context, db DBTX, year, month int, closedBy string) error {
	check, err := CheckClose(ctx, db, year, month)
	if err != nil {
		return err
	}
	if check.Status == "closed" {
		return fmt.Errorf("%d年%02d月已经结账", year, month)
	}
	if !check.CanClose {
		if len(check.Blockers) > 0 {
			return fmt.Errorf("不能结账：%s", check.Blockers[0].Message)
		}
		return fmt.Errorf("不能结账")
	}
	if err := postPLClose(ctx, db, year, month, closedBy); err != nil {
		return err
	}
	if err := carryForward(ctx, db, year, month); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE gl_periods SET status='closed', closed_at=NOW(), closed_by=$3
		WHERE year=$1 AND month=$2 AND company_id=$4 AND status='open'`,
		year, month, closedBy, companyID)
	return err
}

func postPLClose(ctx context.Context, db DBTX, year, month int, by string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	tb, err := TrialBalance(ctx, db, year, month, true)
	if err != nil {
		return err
	}
	var lines []LineInput
	incomeNet := decimal.Zero
	expenseNet := decimal.Zero
	for _, row := range tb.Rows {
		if !row.IsLeaf {
			continue
		}
		endDr, _ := parseMoney(row.EndDebit)
		endCr, _ := parseMoney(row.EndCredit)
		net := endDr.Sub(endCr)
		switch row.Category {
		case "income":
			if net.IsZero() {
				continue
			}
			if net.LessThan(decimal.Zero) {
				amt := net.Neg()
				lines = append(lines, LineInput{AccountCode: row.Code, Summary: "结转本期损益", Debit: amt.StringFixed(2)})
				lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转 " + row.Name, Credit: amt.StringFixed(2)})
				incomeNet = incomeNet.Add(amt)
			} else {
				lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转 " + row.Name, Debit: net.StringFixed(2)})
				lines = append(lines, LineInput{AccountCode: row.Code, Summary: "结转本期损益", Credit: net.StringFixed(2)})
				incomeNet = incomeNet.Sub(net)
			}
		case "expense":
			if net.IsZero() {
				continue
			}
			if net.GreaterThan(decimal.Zero) {
				lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转 " + row.Name, Debit: net.StringFixed(2)})
				lines = append(lines, LineInput{AccountCode: row.Code, Summary: "结转本期损益", Credit: net.StringFixed(2)})
				expenseNet = expenseNet.Add(net)
			} else {
				amt := net.Neg()
				lines = append(lines, LineInput{AccountCode: row.Code, Summary: "结转本期损益", Debit: amt.StringFixed(2)})
				lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转 " + row.Name, Credit: amt.StringFixed(2)})
				expenseNet = expenseNet.Sub(amt)
			}
		}
	}
	profit := incomeNet.Sub(expenseNet)
	if !profit.IsZero() {
		if profit.GreaterThan(decimal.Zero) {
			lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转未分配利润", Debit: profit.StringFixed(2)})
			lines = append(lines, LineInput{AccountCode: settings.RetainedEarnings, Summary: "本期净利润", Credit: profit.StringFixed(2)})
		} else {
			loss := profit.Neg()
			lines = append(lines, LineInput{AccountCode: settings.RetainedEarnings, Summary: "本期净亏损", Debit: loss.StringFixed(2)})
			lines = append(lines, LineInput{AccountCode: settings.IncomeSummary, Summary: "结转未分配利润", Credit: loss.StringFixed(2)})
		}
	}
	if len(lines) == 0 {
		return nil
	}
	end, err := getPeriod(ctx, db, year, month)
	if err != nil {
		return err
	}
	source := periodSourceID(year, month)
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "转",
		VoucherDate: end.EndDate,
		Summary:     fmt.Sprintf("结转%d年%02d月损益", year, month),
		SourceType:  "period_close",
		SourceID:    source.String(),
		Lines:       lines,
	}, by)
	return err
}

func periodSourceID(year, month int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("period_close:%d-%02d", year, month)))
}

func openingSourceID(year int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("opening:%d", year)))
}

func carryForward(ctx context.Context, db DBTX, year, month int) error {
	ny, nm := nextPeriod(year, month)
	if _, err := ensurePeriod(ctx, db, ny, nm); err != nil {
		return err
	}
	tb, err := TrialBalance(ctx, db, year, month, true)
	if err != nil {
		return err
	}
	for _, row := range tb.Rows {
		if !row.IsLeaf {
			continue
		}
		endDr, _ := parseMoney(row.EndDebit)
		endCr, _ := parseMoney(row.EndCredit)
		if err := ensureBalanceRow(ctx, db, ny, nm, row.Code); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE gl_account_balances
			SET opening_debit=$4, opening_credit=$5
			WHERE period_year=$1 AND period_month=$2 AND account_code=$3 AND company_id=$6`,
			ny, nm, row.Code, endDr, endCr, companyID); err != nil {
			return err
		}
	}
	return nil
}

func ReopenPeriod(ctx context.Context, db DBTX, year, month int, by string) error {
	check, err := CheckClose(ctx, db, year, month)
	if err != nil {
		return err
	}
	if check.Status != "closed" {
		return fmt.Errorf("%d年%02d月尚未结账", year, month)
	}
	if !check.CanReopen {
		return fmt.Errorf("下一期间已有凭证，不能反结账")
	}
	if err := ReverseBySourceAllowClosed(ctx, db, "period_close", periodSourceID(year, month), by); err != nil {
		return err
	}
	ny, nm := nextPeriod(year, month)
	if _, err := db.ExecContext(ctx, `
		UPDATE gl_account_balances SET opening_debit=0, opening_credit=0
		WHERE period_year=$1 AND period_month=$2 AND company_id=$3`, ny, nm, companyID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE gl_periods SET status='open', closed_at=NULL, closed_by=''
		WHERE year=$1 AND month=$2 AND company_id=$3`, year, month, companyID)
	return err
}

func SaveOpenings(ctx context.Context, db DBTX, year int, lines []LineInput, preparedBy string) (Voucher, error) {
	p, err := ensurePeriod(ctx, db, year, 1)
	if err != nil {
		return Voucher{}, err
	}
	if p.Status == "closed" {
		return Voucher{}, fmt.Errorf("%d年已有结账期间，不能再录入期初", year)
	}
	var posted int64
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gl_vouchers
		WHERE period_year=$1 AND status='posted' AND source_type <> 'opening'`, year).Scan(&posted); err != nil {
		return Voucher{}, err
	}
	if posted > 0 {
		return Voucher{}, fmt.Errorf("本年度已有业务凭证，不能改期初")
	}
	exists, err := hasSourceVoucher(ctx, db, "opening", openingSourceID(year))
	if err != nil {
		return Voucher{}, err
	}
	if exists {
		if err := undoOpeningShift(ctx, db, year); err != nil {
			return Voucher{}, err
		}
		if err := ReverseBySource(ctx, db, "opening", openingSourceID(year), preparedBy); err != nil {
			return Voucher{}, err
		}
	}
	parsed, _, _, err := parseLines(lines)
	if err != nil {
		return Voucher{}, err
	}
	if err := validateLeafAccounts(ctx, db, parsed); err != nil {
		return Voucher{}, err
	}
	v, err := createAndPost(ctx, db, VoucherInput{
		Word:        "初",
		VoucherDate: fmt.Sprintf("%d-01-01", year),
		Summary:     fmt.Sprintf("%d年科目期初", year),
		SourceType:  "opening",
		SourceID:    openingSourceID(year).String(),
		Lines:       lines,
	}, preparedBy)
	if err != nil {
		return Voucher{}, err
	}
	for _, ln := range parsed {
		if err := ensureBalanceRow(ctx, db, year, 1, ln.AccountCode); err != nil {
			return Voucher{}, err
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE gl_account_balances
			SET opening_debit = opening_debit + $4, opening_credit = opening_credit + $5,
			    period_debit = period_debit - $4, period_credit = period_credit - $5
			WHERE period_year=$1 AND period_month=$2 AND account_code=$3`,
			year, 1, ln.AccountCode, ln.Debit, ln.Credit); err != nil {
			return Voucher{}, err
		}
	}
	return v, nil
}

func undoOpeningShift(ctx context.Context, db DBTX, year int) error {
	var id uuid.UUID
	err := db.QueryRowContext(ctx, `
		SELECT id FROM gl_vouchers
		WHERE source_type='opening' AND source_id=$1 AND reverses_id IS NULL AND reversed_by_id IS NULL AND status='posted'`,
		openingSourceID(year)).Scan(&id)
	if err != nil {
		return err
	}
	v, err := GetVoucher(ctx, db, id)
	if err != nil {
		return err
	}
	for _, ln := range v.Lines {
		if err := ensureBalanceRow(ctx, db, year, 1, ln.AccountCode); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE gl_account_balances
			SET opening_debit = opening_debit - $4, opening_credit = opening_credit - $5,
			    period_debit = period_debit + $4, period_credit = period_credit + $5
			WHERE period_year=$1 AND period_month=$2 AND account_code=$3 AND company_id=$6`,
			year, 1, ln.AccountCode, ln.Debit, ln.Credit, companyID); err != nil {
			return err
		}
	}
	return nil
}
