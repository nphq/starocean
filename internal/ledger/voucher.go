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

func GetVoucher(ctx context.Context, db DBTX, id uuid.UUID) (Voucher, error) {
	v, err := scanVoucher(db.QueryRowContext(ctx, voucherSelect+` WHERE v.id=$1`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return Voucher{}, fmt.Errorf("凭证不存在")
		}
		return Voucher{}, err
	}
	lines, err := loadLines(ctx, db, v.ID)
	if err != nil {
		return Voucher{}, err
	}
	v.Lines = lines
	return v, nil
}

const voucherSelect = `
	SELECT v.id, v.voucher_no, v.word, v.voucher_date::text, v.period_year, v.period_month,
	       v.attachment_count, v.summary, v.status, v.source_type, v.source_id, v.prepared_by,
	       v.posted_at, v.posted_by, v.reverses_id, v.reversed_by_id, v.debit_total, v.credit_total,
	       v.created_at, v.reviewed_by, v.reviewed_at, v.review_note
	FROM gl_vouchers v`

func scanVoucher(row *sql.Row) (Voucher, error) {
	var v Voucher
	var sourceID, reverses, reversed uuid.NullUUID
	var postedAt, reviewedAt sql.NullTime
	err := row.Scan(&v.ID, &v.VoucherNo, &v.Word, &v.VoucherDate, &v.PeriodYear, &v.PeriodMonth,
		&v.AttachmentCount, &v.Summary, &v.Status, &v.SourceType, &sourceID, &v.PreparedBy,
		&postedAt, &v.PostedBy, &reverses, &reversed, &v.DebitTotal, &v.CreditTotal, &v.CreatedAt,
		&v.ReviewedBy, &reviewedAt, &v.ReviewNote)
	if err != nil {
		return v, err
	}
	if sourceID.Valid {
		id := sourceID.UUID
		v.SourceID = &id
	}
	if postedAt.Valid {
		t := postedAt.Time
		v.PostedAt = &t
	}
	if reviewedAt.Valid {
		t := reviewedAt.Time
		v.ReviewedAt = &t
	}
	if reverses.Valid {
		id := reverses.UUID
		v.ReversesID = &id
	}
	if reversed.Valid {
		id := reversed.UUID
		v.ReversedByID = &id
	}
	return v, nil
}

func loadLines(ctx context.Context, db DBTX, voucherID uuid.UUID) ([]VoucherLine, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT l.id, l.line_no, l.account_code, a.name, l.summary, l.debit, l.credit,
		       l.partner_type, l.partner_id, l.partner_name
		FROM gl_voucher_lines l
		JOIN gl_accounts a ON a.code = l.account_code
		WHERE l.voucher_id = $1
		ORDER BY l.line_no`, voucherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []VoucherLine
	for rows.Next() {
		var ln VoucherLine
		var partnerID uuid.NullUUID
		if err := rows.Scan(&ln.ID, &ln.LineNo, &ln.AccountCode, &ln.AccountName, &ln.Summary,
			&ln.Debit, &ln.Credit, &ln.PartnerType, &partnerID, &ln.PartnerName); err != nil {
			return nil, err
		}
		if partnerID.Valid {
			id := partnerID.UUID
			ln.PartnerID = &id
		}
		lines = append(lines, ln)
	}
	return lines, rows.Err()
}

func ListVouchers(ctx context.Context, db DBTX, year, month int, status, q string, page, pageSize int) ([]Voucher, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	where := []string{"v.company_id = $1"}
	args := []interface{}{companyID}
	n := 2
	if year > 0 {
		where = append(where, fmt.Sprintf("v.period_year = $%d", n))
		args = append(args, year)
		n++
	}
	if month > 0 {
		where = append(where, fmt.Sprintf("v.period_month = $%d", n))
		args = append(args, month)
		n++
	}
	if status != "" && status != "all" {
		where = append(where, fmt.Sprintf("v.status = $%d", n))
		args = append(args, status)
		n++
	}
	if q != "" {
		where = append(where, fmt.Sprintf("(v.voucher_no ILIKE $%d OR v.summary ILIKE $%d)", n, n))
		args = append(args, "%"+q+"%")
		n++
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gl_vouchers v WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := db.QueryContext(ctx, voucherSelect+` WHERE `+clause+
		fmt.Sprintf(` ORDER BY v.voucher_date DESC, v.voucher_no DESC LIMIT $%d OFFSET $%d`, n, n+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Voucher
	for rows.Next() {
		v, err := scanVoucherRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func scanVoucherRow(rows *sql.Rows) (Voucher, error) {
	var v Voucher
	var sourceID, reverses, reversed uuid.NullUUID
	var postedAt, reviewedAt sql.NullTime
	err := rows.Scan(&v.ID, &v.VoucherNo, &v.Word, &v.VoucherDate, &v.PeriodYear, &v.PeriodMonth,
		&v.AttachmentCount, &v.Summary, &v.Status, &v.SourceType, &sourceID, &v.PreparedBy,
		&postedAt, &v.PostedBy, &reverses, &reversed, &v.DebitTotal, &v.CreditTotal, &v.CreatedAt,
		&v.ReviewedBy, &reviewedAt, &v.ReviewNote)
	if err != nil {
		return v, err
	}
	if sourceID.Valid {
		id := sourceID.UUID
		v.SourceID = &id
	}
	if postedAt.Valid {
		t := postedAt.Time
		v.PostedAt = &t
	}
	if reviewedAt.Valid {
		t := reviewedAt.Time
		v.ReviewedAt = &t
	}
	if reverses.Valid {
		id := reverses.UUID
		v.ReversesID = &id
	}
	if reversed.Valid {
		id := reversed.UUID
		v.ReversedByID = &id
	}
	return v, nil
}

func ensureBalanceRow(ctx context.Context, db DBTX, year, month int, code string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO gl_account_balances (period_year, period_month, account_code)
		VALUES ($1,$2,$3)
		ON CONFLICT (period_year, period_month, account_code, company_id) DO NOTHING`,
		year, month, code)
	return err
}

func applyMovement(ctx context.Context, db DBTX, year, month int, code string, debit, credit decimal.Decimal) error {
	if err := ensureBalanceRow(ctx, db, year, month, code); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE gl_account_balances
		SET period_debit = period_debit + $4, period_credit = period_credit + $5
		WHERE period_year=$1 AND period_month=$2 AND account_code=$3 AND company_id=$6`,
		year, month, code, debit, credit, companyID)
	return err
}

func PostVoucher(ctx context.Context, db DBTX, id uuid.UUID, postedBy string) error {
	return postVoucher(ctx, db, id, postedBy, false, false)
}

// postVoucher 过账凭证。skipReview=true 用于自动制单/红冲（确定性单据，无需审核，
// 含 review 开启时也直接过账，对标 SAP 集成凭证直接过账）；false 时按
// require_review 开关分流：开启仅接受 reviewed，关闭接受 draft/reviewed（单步兼容）。
func postVoucher(ctx context.Context, db DBTX, id uuid.UUID, postedBy string, allowClosed, skipReview bool) error {
	v, err := GetVoucher(ctx, db, id)
	if err != nil {
		return err
	}
	if skipReview {
		if v.Status != "draft" {
			return fmt.Errorf("只有草稿凭证可以过账")
		}
	} else {
		st, err := loadSettings(ctx, db)
		if err != nil {
			return fmt.Errorf("读取总账设置: %w", err)
		}
		if st.RequireReview {
			if v.Status != "reviewed" {
				return fmt.Errorf("凭证未审核，不能过账")
			}
		} else {
			if v.Status != "draft" && v.Status != "reviewed" {
				return fmt.Errorf("只有草稿凭证可以过账")
			}
		}
	}
	if !allowClosed {
		if err := requireOpenPeriod(ctx, db, v.PeriodYear, v.PeriodMonth); err != nil {
			return err
		}
	}
	if !v.DebitTotal.Equal(v.CreditTotal) {
		return fmt.Errorf("借贷不平衡，不能过账")
	}
	for _, ln := range v.Lines {
		if err := applyMovement(ctx, db, v.PeriodYear, v.PeriodMonth, ln.AccountCode, ln.Debit, ln.Credit); err != nil {
			return err
		}
	}
	res, err := db.ExecContext(ctx, `
		UPDATE gl_vouchers SET status='posted', posted_at=NOW(), posted_by=$2, updated_at=NOW()
		WHERE id=$1 AND status IN ('draft','reviewed')`, id, postedBy)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("过账失败，凭证状态已变化")
	}
	return nil
}

// ReviewVoucher 审核：draft → reviewed。审核人不得为制单人；仅审核借贷平衡的草稿凭证。
func ReviewVoucher(ctx context.Context, db DBTX, id uuid.UUID, reviewedBy, note string) error {
	v, err := GetVoucher(ctx, db, id)
	if err != nil {
		return err
	}
	if v.Status != "draft" {
		return fmt.Errorf("只有草稿凭证可以审核")
	}
	if reviewedBy != "" && v.PreparedBy != "" && reviewedBy == v.PreparedBy {
		return fmt.Errorf("审核人不能与制单人为同一人")
	}
	if !v.DebitTotal.Equal(v.CreditTotal) {
		return fmt.Errorf("借贷不平衡，不能审核")
	}
	_, err = db.ExecContext(ctx, `
		UPDATE gl_vouchers SET status='reviewed', reviewed_by=$2, reviewed_at=NOW(), review_note=$3, updated_at=NOW()
		WHERE id=$1 AND status='draft'`, id, reviewedBy, note)
	if err != nil {
		return err
	}
	return nil
}

// RejectVoucher 驳回：reviewed → draft，必填驳回原因写入 review_note。
func RejectVoucher(ctx context.Context, db DBTX, id uuid.UUID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("驳回原因不能为空")
	}
	v, err := GetVoucher(ctx, db, id)
	if err != nil {
		return err
	}
	if v.Status != "reviewed" {
		return fmt.Errorf("只有已审核凭证可以驳回")
	}
	_, err = db.ExecContext(ctx, `
		UPDATE gl_vouchers SET status='draft', reviewed_by='', reviewed_at=NULL, review_note=$2, updated_at=NOW()
		WHERE id=$1 AND status='reviewed'`, id, reason)
	if err != nil {
		return err
	}
	return nil
}

func ReverseVoucher(ctx context.Context, db DBTX, id uuid.UUID, preparedBy string) (Voucher, error) {
	return reverseVoucher(ctx, db, id, preparedBy, false)
}

func reverseVoucher(ctx context.Context, db DBTX, id uuid.UUID, preparedBy string, allowClosed bool) (Voucher, error) {
	orig, err := GetVoucher(ctx, db, id)
	if err != nil {
		return Voucher{}, err
	}
	if orig.Status != "posted" {
		return Voucher{}, fmt.Errorf("只能冲销已过账凭证")
	}
	if orig.ReversedByID != nil {
		return Voucher{}, fmt.Errorf("该凭证已被冲销")
	}
	date, err := parseDate(orig.VoucherDate)
	if err != nil {
		date = time.Now()
	}
	year, month := periodOf(date)
	if !allowClosed {
		if err := requireOpenPeriod(ctx, db, year, month); err != nil {
			date = time.Now()
			year, month = periodOf(date)
			if err := requireOpenPeriod(ctx, db, year, month); err != nil {
				return Voucher{}, err
			}
		}
	}
	lines := make([]LineInput, 0, len(orig.Lines))
	for _, ln := range orig.Lines {
		pid := ""
		if ln.PartnerID != nil {
			pid = ln.PartnerID.String()
		}
		lines = append(lines, LineInput{
			AccountCode: ln.AccountCode,
			Summary:     "冲销 " + orig.VoucherNo,
			Debit:       ln.Credit.StringFixed(2),
			Credit:      ln.Debit.StringFixed(2),
			PartnerType: ln.PartnerType,
			PartnerID:   pid,
			PartnerName: ln.PartnerName,
		})
	}
	parsed, dr, cr, err := parseLines(lines)
	if err != nil {
		return Voucher{}, err
	}
	no, err := nextVoucherNo(ctx, db, orig.Word, year, month)
	if err != nil {
		return Voucher{}, err
	}
	revID := uuid.New()
	rev := Voucher{
		ID:          revID,
		VoucherNo:   no,
		Word:        orig.Word,
		VoucherDate: dateStr(date),
		PeriodYear:  year,
		PeriodMonth: month,
		Summary:     "冲销 " + orig.VoucherNo + " " + orig.Summary,
		Status:      "draft",
		SourceType:  orig.SourceType,
		SourceID:    orig.SourceID,
		PreparedBy:  preparedBy,
		ReversesID:  &orig.ID,
		DebitTotal:  dr,
		CreditTotal: cr,
	}
	if err := insertVoucher(ctx, db, rev, parsed); err != nil {
		return Voucher{}, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE gl_vouchers SET reversed_by_id=$2, updated_at=NOW() WHERE id=$1`, orig.ID, revID); err != nil {
		return Voucher{}, err
	}
	if err := postVoucher(ctx, db, revID, preparedBy, allowClosed, true); err != nil {
		return Voucher{}, err
	}
	return GetVoucher(ctx, db, revID)
}

func ReverseBySource(ctx context.Context, db DBTX, sourceType string, sourceID uuid.UUID, preparedBy string) error {
	return reverseBySource(ctx, db, sourceType, sourceID, preparedBy, false)
}

func ReverseBySourceAllowClosed(ctx context.Context, db DBTX, sourceType string, sourceID uuid.UUID, preparedBy string) error {
	return reverseBySource(ctx, db, sourceType, sourceID, preparedBy, true)
}

func reverseBySource(ctx context.Context, db DBTX, sourceType string, sourceID uuid.UUID, preparedBy string, allowClosed bool) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id FROM gl_vouchers
		WHERE source_type=$1 AND source_id=$2 AND status='posted' AND reverses_id IS NULL AND reversed_by_id IS NULL`,
		sourceType, sourceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := reverseVoucher(ctx, db, id, preparedBy, allowClosed); err != nil {
			return err
		}
	}
	return nil
}

func createAndPost(ctx context.Context, db DBTX, in VoucherInput, preparedBy string) (Voucher, error) {
	v, err := CreateVoucher(ctx, db, in, preparedBy)
	if err != nil {
		return Voucher{}, err
	}
	// 自动制单/业务事件：直接过账，不走审核（skipReview）。开关开启时依然生效。
	if err := postVoucher(ctx, db, v.ID, preparedBy, false, true); err != nil {
		return Voucher{}, err
	}
	return GetVoucher(ctx, db, v.ID)
}

func hasSourceVoucher(ctx context.Context, db DBTX, sourceType string, sourceID uuid.UUID) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gl_vouchers
		WHERE source_type=$1 AND source_id=$2 AND reverses_id IS NULL AND reversed_by_id IS NULL AND status IN ('draft','reviewed','posted')`,
		sourceType, sourceID).Scan(&n)
	return n > 0, err
}
