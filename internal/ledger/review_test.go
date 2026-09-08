package ledger

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func setRequireReview(t *testing.T, db DBTX) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `UPDATE gl_settings SET require_review = 1 WHERE company_id = 'default'`); err != nil {
		t.Fatalf("set require_review: %v", err)
	}
}

func draftVoucher(ctx context.Context, t *testing.T, db DBTX, preparedBy string) uuid.UUID {
	t.Helper()
	v, err := CreateVoucher(ctx, db, VoucherInput{
		VoucherDate: "2024-05-12",
		Summary:     "待审核凭证",
		Lines: []LineInput{
			{AccountCode: "1002", Summary: "行1", Debit: "100.00"},
			{AccountCode: "6001", Summary: "行2", Credit: "100.00"},
		},
	}, preparedBy)
	if err != nil {
		t.Fatalf("create voucher: %v", err)
	}
	return v.ID
}

func TestReviewFlowAndPostable(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	id := draftVoucher(ctx, t, tx, "alice")
	if err := ReviewVoucher(ctx, tx, id, "bob", "OK"); err != nil {
		t.Fatalf("review: %v", err)
	}
	v, err := GetVoucher(ctx, tx, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "reviewed" || v.ReviewedBy != "bob" || v.ReviewNote != "OK" || v.ReviewedAt == nil {
		t.Fatalf("unexpected review state: status=%s reviewed_by=%s note=%s", v.Status, v.ReviewedBy, v.ReviewNote)
	}
	if err := PostVoucher(ctx, tx, id, "bob"); err != nil {
		t.Fatalf("post reviewed voucher: %v", err)
	}
	v, _ = GetVoucher(ctx, tx, id)
	if v.Status != "posted" {
		t.Fatalf("expected posted, got %s", v.Status)
	}
}

func TestReviewSelfRejected(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	id := draftVoucher(ctx, t, tx, "alice")
	err = ReviewVoucher(ctx, tx, id, "alice", "")
	if err == nil || !strings.Contains(err.Error(), "不能与制单人") {
		t.Fatalf("expected self-review to be rejected, got %v", err)
	}
}

func TestReviewRejectsUnbalanced(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// 直接插入一张借贷不平的草稿（绕过 CreateVoucher 的平衡校验），验证审核环节仍拦截。
	id := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO gl_vouchers (id, voucher_no, voucher_date, period_year, period_month, status, prepared_by, debit_total, credit_total)
		VALUES ($1, 'UNBAL-1', '2024-05-12', 2024, 5, 'draft', 'alice', 100, 80)`, id.String()); err != nil {
		t.Fatalf("insert unbalanced draft: %v", err)
	}
	if err := ReviewVoucher(ctx, tx, id, "bob", ""); err == nil || !strings.Contains(err.Error(), "借贷不平衡") {
		t.Fatalf("expected unbalanced to be rejected, got %v", err)
	}
}

func TestRejectFlow(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	id := draftVoucher(ctx, t, tx, "alice")
	if err := ReviewVoucher(ctx, tx, id, "bob", "OK"); err != nil {
		t.Fatal(err)
	}
	if err := RejectVoucher(ctx, tx, id, "科目用错"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	v, _ := GetVoucher(ctx, tx, id)
	if v.Status != "draft" || v.ReviewNote != "科目用错" || v.ReviewedBy != "" || v.ReviewedAt != nil {
		t.Fatalf("unexpected reject state: status=%s note=%s reviewed_by=%s", v.Status, v.ReviewNote, v.ReviewedBy)
	}
	// 驳回原因必填
	if err := RejectVoucher(ctx, tx, id, "  "); err == nil {
		t.Fatal("expected empty reason to be rejected")
	}
}

func TestRequireReviewGate(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	setRequireReview(t, tx)

	id := draftVoucher(ctx, t, tx, "alice")
	// 未审核直接过账应被拒
	if err := PostVoucher(ctx, tx, id, "alice"); err == nil || !strings.Contains(err.Error(), "未审核") {
		t.Fatalf("expected review-gated post to be rejected, got %v", err)
	}
	// 审核后过账成功
	if err := ReviewVoucher(ctx, tx, id, "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, id, "bob"); err != nil {
		t.Fatalf("post after review: %v", err)
	}
}

func TestAutoVoucherBypassesReview(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	setRequireReview(t, tx)

	// 自动制单走 createAndPost（如销售确认/收付款等），即使 require_review 开启也应直接过账。
	v, err := createAndPost(ctx, tx, VoucherInput{
		VoucherDate: "2024-05-12",
		Summary:     "自动凭证",
		SourceType:  "sales_order",
		SourceID:    uuid.NewString(),
		Lines: []LineInput{
			{AccountCode: "1122", Debit: "200.00"},
			{AccountCode: "6001", Credit: "200.00"},
		},
	}, "system")
	if err != nil {
		t.Fatalf("auto post bypass review: %v", err)
	}
	if v.Status != "posted" {
		t.Fatalf("expected auto voucher to be posted, got %s", v.Status)
	}
}

func TestCloseBlocksReviewed(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	setRequireReview(t, tx)
	id := draftVoucher(ctx, t, tx, "alice")
	if err := ReviewVoucher(ctx, tx, id, "bob", ""); err != nil {
		t.Fatal(err)
	}
	chk, err := CheckClose(ctx, tx, 2024, 5)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range chk.Blockers {
		if b.Code == "drafts" && b.Count >= 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected close to be blocked by reviewed voucher, blockers=%+v", chk.Blockers)
	}
	if chk.ReviewedCount < 1 {
		t.Fatalf("expected reviewed_count >= 1, got %d", chk.ReviewedCount)
	}
}
