package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func GetAccount(ctx context.Context, db DBTX, code string) (Account, error) {
	var a Account
	err := db.QueryRowContext(ctx, `
		SELECT code, name, parent_code, category, normal_side, is_leaf, is_cash, aux_ar, aux_ap, active, sort_order
		FROM gl_accounts WHERE code = $1`, code).Scan(
		&a.Code, &a.Name, &a.ParentCode, &a.Category, &a.NormalSide, &a.IsLeaf, &a.IsCash,
		&a.AuxAR, &a.AuxAP, &a.Active, &a.SortOrder)
	if err != nil {
		if err == sql.ErrNoRows {
			return a, fmt.Errorf("科目 %s 不存在", code)
		}
		return a, err
	}
	return a, nil
}

func ListAccounts(ctx context.Context, db DBTX, q string, category string, leavesOnly bool) ([]Account, error) {
	sqlStr := `
		SELECT code, name, parent_code, category, normal_side, is_leaf, is_cash, aux_ar, aux_ap, active, sort_order
		FROM gl_accounts WHERE company_id = $1`
	args := []interface{}{companyID}
	n := 2
	if category != "" {
		sqlStr += fmt.Sprintf(" AND category = $%d", n)
		args = append(args, category)
		n++
	}
	if leavesOnly {
		sqlStr += " AND is_leaf = TRUE AND active = TRUE"
	}
	if q != "" {
		sqlStr += fmt.Sprintf(" AND (code ILIKE $%d OR name ILIKE $%d)", n, n)
		args = append(args, "%"+q+"%")
	}
	sqlStr += " ORDER BY sort_order, code"
	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Code, &a.Name, &a.ParentCode, &a.Category, &a.NormalSide, &a.IsLeaf, &a.IsCash,
			&a.AuxAR, &a.AuxAP, &a.Active, &a.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func UpsertAccount(ctx context.Context, db DBTX, a Account) error {
	if a.Code == "" || a.Name == "" {
		return fmt.Errorf("科目编码和名称不能为空")
	}
	if a.Category == "" || a.NormalSide == "" {
		return fmt.Errorf("科目类别和余额方向不能为空")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO gl_accounts (code, name, parent_code, category, normal_side, is_leaf, is_cash, aux_ar, aux_ap, active, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name, parent_code = EXCLUDED.parent_code, category = EXCLUDED.category,
			normal_side = EXCLUDED.normal_side, is_leaf = EXCLUDED.is_leaf, is_cash = EXCLUDED.is_cash,
			aux_ar = EXCLUDED.aux_ar, aux_ap = EXCLUDED.aux_ap, active = EXCLUDED.active, sort_order = EXCLUDED.sort_order`,
		a.Code, a.Name, a.ParentCode, a.Category, a.NormalSide, a.IsLeaf, a.IsCash, a.AuxAR, a.AuxAP, a.Active, a.SortOrder)
	return err
}

func ListPeriods(ctx context.Context, db DBTX, year int) ([]Period, error) {
	q := `SELECT year, month, start_date::text, end_date::text, status, closed_at, closed_by
		FROM gl_periods WHERE company_id = $1`
	args := []interface{}{companyID}
	if year > 0 {
		q += " AND year = $2"
		args = append(args, year)
	}
	q += " ORDER BY year, month"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Period
	for rows.Next() {
		var p Period
		var closedAt sql.NullTime
		if err := rows.Scan(&p.Year, &p.Month, &p.StartDate, &p.EndDate, &p.Status, &closedAt, &p.ClosedBy); err != nil {
			return nil, err
		}
		if closedAt.Valid {
			t := closedAt.Time
			p.ClosedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func getPeriod(ctx context.Context, db DBTX, year, month int) (Period, error) {
	var p Period
	var closedAt sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT year, month, start_date::text, end_date::text, status, closed_at, closed_by
		FROM gl_periods WHERE year=$1 AND month=$2 AND company_id=$3`,
		year, month, companyID).Scan(&p.Year, &p.Month, &p.StartDate, &p.EndDate, &p.Status, &closedAt, &p.ClosedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return p, fmt.Errorf("会计期间 %d-%02d 不存在", year, month)
		}
		return p, err
	}
	if closedAt.Valid {
		t := closedAt.Time
		p.ClosedAt = &t
	}
	return p, nil
}

func ensurePeriod(ctx context.Context, db DBTX, year, month int) (Period, error) {
	p, err := getPeriod(ctx, db, year, month)
	if err == nil {
		return p, nil
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	_, err = db.ExecContext(ctx, `
		INSERT INTO gl_periods (year, month, start_date, end_date, status)
		VALUES ($1,$2,$3,$4,'open')
		ON CONFLICT (year, month, company_id) DO NOTHING`,
		year, month, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return p, err
	}
	return getPeriod(ctx, db, year, month)
}

func requireOpenPeriod(ctx context.Context, db DBTX, year, month int) error {
	p, err := ensurePeriod(ctx, db, year, month)
	if err != nil {
		return err
	}
	if p.Status == "closed" {
		return fmt.Errorf("%d年%02d月已结账，不能记账", year, month)
	}
	return nil
}

func nextVoucherNo(ctx context.Context, db DBTX, word string, year, month int) (string, error) {
	if word == "" {
		word = "记"
	}
	key := fmt.Sprintf("%d%02d", year, month)
	var seq int
	err := db.QueryRowContext(ctx, `
		INSERT INTO gl_voucher_seq (period_key, word, last_seq) VALUES ($1,$2,1)
		ON CONFLICT (period_key, word, company_id) DO UPDATE SET last_seq = gl_voucher_seq.last_seq + 1
		RETURNING last_seq`, key, word).Scan(&seq)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", word, key, seq), nil
}

func parseLines(in []LineInput) ([]VoucherLine, decimal.Decimal, decimal.Decimal, error) {
	var lines []VoucherLine
	debitTotal := decimal.Zero
	creditTotal := decimal.Zero
	n := 0
	for _, raw := range in {
		if strings.TrimSpace(raw.AccountCode) == "" {
			continue
		}
		dr, err := parseMoney(raw.Debit)
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, err
		}
		cr, err := parseMoney(raw.Credit)
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, err
		}
		if dr.IsZero() && cr.IsZero() {
			continue
		}
		if dr.GreaterThan(decimal.Zero) && cr.GreaterThan(decimal.Zero) {
			return nil, decimal.Zero, decimal.Zero, fmt.Errorf("同一分录不能同时有借方和贷方")
		}
		n++
		line := VoucherLine{
			ID:          uuid.New(),
			LineNo:      n,
			AccountCode: strings.TrimSpace(raw.AccountCode),
			Summary:     raw.Summary,
			Debit:       dr,
			Credit:      cr,
			PartnerType: raw.PartnerType,
			PartnerName: raw.PartnerName,
		}
		if raw.PartnerID != "" {
			id, err := uuid.Parse(raw.PartnerID)
			if err == nil {
				line.PartnerID = &id
			}
		}
		lines = append(lines, line)
		debitTotal = debitTotal.Add(dr)
		creditTotal = creditTotal.Add(cr)
	}
	if len(lines) < 2 {
		return nil, decimal.Zero, decimal.Zero, fmt.Errorf("凭证至少需要两行分录")
	}
	if !debitTotal.Equal(creditTotal) {
		return nil, decimal.Zero, decimal.Zero, fmt.Errorf("借贷不平衡：借方 %s，贷方 %s", debitTotal.StringFixed(2), creditTotal.StringFixed(2))
	}
	if debitTotal.LessThanOrEqual(decimal.Zero) {
		return nil, decimal.Zero, decimal.Zero, fmt.Errorf("凭证金额必须大于零")
	}
	return lines, money(debitTotal), money(creditTotal), nil
}

func validateLeafAccounts(ctx context.Context, db DBTX, lines []VoucherLine) error {
	for _, ln := range lines {
		a, err := GetAccount(ctx, db, ln.AccountCode)
		if err != nil {
			return err
		}
		if !a.Active {
			return fmt.Errorf("科目 %s %s 已停用", a.Code, a.Name)
		}
		if !a.IsLeaf {
			return fmt.Errorf("科目 %s %s 不是明细科目，不能入账", a.Code, a.Name)
		}
	}
	return nil
}

func insertVoucher(ctx context.Context, db DBTX, v Voucher, lines []VoucherLine) error {
	var sourceID interface{}
	if v.SourceID != nil {
		sourceID = *v.SourceID
	}
	var reverses interface{}
	if v.ReversesID != nil {
		reverses = *v.ReversesID
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO gl_vouchers (
			id, voucher_no, word, voucher_date, period_year, period_month, attachment_count,
			summary, status, source_type, source_id, prepared_by, reverses_id, debit_total, credit_total
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		v.ID, v.VoucherNo, v.Word, v.VoucherDate, v.PeriodYear, v.PeriodMonth, v.AttachmentCount,
		v.Summary, v.Status, v.SourceType, sourceID, v.PreparedBy, reverses, v.DebitTotal, v.CreditTotal)
	if err != nil {
		return err
	}
	return insertLines(ctx, db, v.ID, lines)
}

func insertLines(ctx context.Context, db DBTX, voucherID uuid.UUID, lines []VoucherLine) error {
	for _, ln := range lines {
		var partnerID interface{}
		if ln.PartnerID != nil {
			partnerID = *ln.PartnerID
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO gl_voucher_lines (id, voucher_id, line_no, account_code, summary, debit, credit, partner_type, partner_id, partner_name)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			ln.ID, voucherID, ln.LineNo, ln.AccountCode, ln.Summary, ln.Debit, ln.Credit, ln.PartnerType, partnerID, ln.PartnerName); err != nil {
			return err
		}
	}
	return nil
}

func CreateVoucher(ctx context.Context, db DBTX, in VoucherInput, preparedBy string) (Voucher, error) {
	dt, err := parseDate(in.VoucherDate)
	if err != nil {
		return Voucher{}, err
	}
	year, month := periodOf(dt)
	if err := requireOpenPeriod(ctx, db, year, month); err != nil {
		return Voucher{}, err
	}
	lines, dr, cr, err := parseLines(in.Lines)
	if err != nil {
		return Voucher{}, err
	}
	if err := validateLeafAccounts(ctx, db, lines); err != nil {
		return Voucher{}, err
	}
	word := in.Word
	if word == "" {
		word = "记"
	}
	no, err := nextVoucherNo(ctx, db, word, year, month)
	if err != nil {
		return Voucher{}, err
	}
	v := Voucher{
		ID:              uuid.New(),
		VoucherNo:       no,
		Word:            word,
		VoucherDate:     dateStr(dt),
		PeriodYear:      year,
		PeriodMonth:     month,
		AttachmentCount: in.AttachmentCount,
		Summary:         in.Summary,
		Status:          "draft",
		SourceType:      in.SourceType,
		PreparedBy:      preparedBy,
		DebitTotal:      dr,
		CreditTotal:     cr,
		CreatedAt:       time.Now(),
		Lines:           lines,
	}
	if in.SourceID != "" {
		id, err := uuid.Parse(in.SourceID)
		if err == nil {
			v.SourceID = &id
		}
	}
	if err := insertVoucher(ctx, db, v, lines); err != nil {
		return Voucher{}, err
	}
	return GetVoucher(ctx, db, v.ID)
}

func UpdateVoucher(ctx context.Context, db DBTX, id uuid.UUID, in VoucherInput) (Voucher, error) {
	cur, err := GetVoucher(ctx, db, id)
	if err != nil {
		return Voucher{}, err
	}
	if cur.Status != "draft" {
		return Voucher{}, fmt.Errorf("只能修改草稿凭证")
	}
	dt, err := parseDate(in.VoucherDate)
	if err != nil {
		return Voucher{}, err
	}
	year, month := periodOf(dt)
	if err := requireOpenPeriod(ctx, db, year, month); err != nil {
		return Voucher{}, err
	}
	lines, dr, cr, err := parseLines(in.Lines)
	if err != nil {
		return Voucher{}, err
	}
	if err := validateLeafAccounts(ctx, db, lines); err != nil {
		return Voucher{}, err
	}
	summary := in.Summary
	if summary == "" {
		summary = cur.Summary
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE gl_vouchers SET voucher_date=$2, period_year=$3, period_month=$4, attachment_count=$5,
			summary=$6, debit_total=$7, credit_total=$8, updated_at=NOW()
		WHERE id=$1 AND status='draft'`,
		id, dateStr(dt), year, month, in.AttachmentCount, summary, dr, cr); err != nil {
		return Voucher{}, err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM gl_voucher_lines WHERE voucher_id=$1`, id); err != nil {
		return Voucher{}, err
	}
	if err := insertLines(ctx, db, id, lines); err != nil {
		return Voucher{}, err
	}
	return GetVoucher(ctx, db, id)
}

func DeleteVoucher(ctx context.Context, db DBTX, id uuid.UUID) error {
	res, err := db.ExecContext(ctx, `DELETE FROM gl_vouchers WHERE id=$1 AND status='draft'`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("只能删除草稿凭证")
	}
	return nil
}
