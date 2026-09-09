package ledger

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	// 缺省使用 t.TempDir() 下的 SQLite 临时库（零依赖，裸 `go test` 也能真正
	// 执行）；STAROCEAN_TEST_DSN 可切换 SQLite/PostgreSQL 运行整套集成测试。
	// 连接失败一律 Fatal：显式 DSN 连不上是配置错误，缺省 SQLite 本地一定可用，
	// 静默 Skip 只会造成假绿（性能基线测试缺库跳过除外，见 main_test ensurePerfDB）。
	url := os.Getenv("STAROCEAN_TEST_DSN")
	if url == "" {
		url = "sqlite:" + filepath.Join(t.TempDir(), "starocean-test.db")
	}
	// go test ./... 各包并行运行，SQLite 需使用独立文件避免相互冲突
	if strings.HasPrefix(url, "sqlite:") {
		p := strings.TrimSuffix(strings.TrimPrefix(url, "sqlite:"), ".db") + "_ledger.db"
		url = "sqlite:" + p
	}
	database, err := db.Connect(url)
	if err != nil {
		t.Fatalf("test database unavailable (DSN=%q): %v", url, err)
	}
	// P0: 自动建库（基线 schema，幂等），此前未迁移直接 Skip 导致 GL 集成测试静默漏跑
	if err := db.Migrate(database); err != nil {
		database.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		database.Exec(`DELETE FROM gl_voucher_lines`)
		database.Exec(`DELETE FROM gl_vouchers`)
		database.Exec(`DELETE FROM gl_account_balances`)
		database.Exec(`DELETE FROM gl_voucher_seq`)
		database.Exec(`UPDATE gl_periods SET status='open', closed_at=NULL, closed_by=''`)
		database.Close()
	})
	return database
}

func TestUnbalancedRejected(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	_, err := CreateVoucher(ctx, database, VoucherInput{
		VoucherDate: "2024-03-10",
		Summary:     "不平",
		Lines: []LineInput{
			{AccountCode: "1002", Debit: "100"},
			{AccountCode: "6001", Credit: "80"},
		},
	}, "test")
	if err == nil {
		t.Fatal("expected unbalanced error")
	}
}

func TestPostAndTrialBalance(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	v, err := CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-03-10",
		Summary:     "收到货款",
		Lines: []LineInput{
			{AccountCode: "1002", Summary: "收款", Debit: "1000.00"},
			{AccountCode: "1122", Summary: "冲应收", Credit: "1000.00"},
		},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, v.ID, "test"); err != nil {
		t.Fatal(err)
	}
	tb, err := TrialBalance(ctx, tx, 2024, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if !tb.Balanced {
		t.Fatalf("trial not balanced: %s / %s", tb.DebitTotal, tb.CreditTotal)
	}
}

func TestMonthCloseAndReopen(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	rev, err := CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-04-08",
		Summary:     "销售收入",
		Lines: []LineInput{
			{AccountCode: "1122", Debit: "500.00"},
			{AccountCode: "6001", Credit: "500.00"},
		},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, rev.ID, "test"); err != nil {
		t.Fatal(err)
	}
	cogs, err := CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-04-08",
		Summary:     "结转成本",
		Lines: []LineInput{
			{AccountCode: "6401", Debit: "200.00"},
			{AccountCode: "1405", Credit: "200.00"},
		},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, cogs.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if err := ClosePeriod(ctx, tx, 2024, 4, "test"); err == nil {
		t.Fatal("expected skip-month close to fail while March is open")
	}
	for m := 1; m <= 3; m++ {
		if err := ClosePeriod(ctx, tx, 2024, m, "test"); err != nil {
			t.Fatalf("close empty %d: %v", m, err)
		}
	}
	if err := ClosePeriod(ctx, tx, 2024, 4, "test"); err != nil {
		t.Fatal(err)
	}
	p, err := getPeriod(ctx, tx, 2024, 4)
	if err != nil || p.Status != "closed" {
		t.Fatalf("period not closed: %+v %v", p, err)
	}
	_, err = CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-04-20",
		Summary:     "应被拒绝",
		Lines: []LineInput{
			{AccountCode: "1001", Debit: "1.00"},
			{AccountCode: "6001", Credit: "1.00"},
		},
	}, "test")
	if err == nil {
		t.Fatal("expected closed period reject")
	}
	tb, err := TrialBalance(ctx, tx, 2024, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if !tb.Balanced {
		t.Fatalf("after close not balanced: %s/%s", tb.DebitTotal, tb.CreditTotal)
	}
	var incDr, incCr string
	for _, r := range tb.Rows {
		if r.Code == "6001" && (r.EndDebit != "0.00" || r.EndCredit != "0.00") {
			t.Fatalf("收入科目结转后应无余额: %+v", r)
		}
		if r.Code == "6401" && (r.EndDebit != "0.00" || r.EndCredit != "0.00") {
			t.Fatalf("成本科目结转后应无余额: %+v", r)
		}
		if r.Code == "410401" {
			incDr, incCr = r.EndDebit, r.EndCredit
		}
	}
	if incCr != "300.00" || incDr != "0.00" {
		t.Fatalf("未分配利润应为贷 300，得到 借%s 贷%s", incDr, incCr)
	}
	pl, err := IncomeStatement(ctx, tx, 2024, 4)
	if err != nil {
		t.Fatal(err)
	}
	var revAmt, netAmt string
	for _, ln := range pl.Lines {
		if ln.Key == "rev" {
			revAmt = ln.Amount
		}
		if ln.Key == "net" {
			netAmt = ln.Amount
		}
	}
	if revAmt != "500.00" {
		t.Fatalf("结账后利润表营业收入应为 500，得到 %s", revAmt)
	}
	if netAmt != "300.00" {
		t.Fatalf("结账后利润表净利润应为 300，得到 %s", netAmt)
	}
	if err := ReopenPeriod(ctx, tx, 2024, 4, "test"); err != nil {
		t.Fatal(err)
	}
	p, _ = getPeriod(ctx, tx, 2024, 4)
	if p.Status != "open" {
		t.Fatal("reopen failed")
	}
	tb, err = TrialBalance(ctx, tx, 2024, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	var revDr, revCr, cogsDr, cogsCr string
	for _, r := range tb.Rows {
		if r.Code == "6001" {
			revDr, revCr = r.EndDebit, r.EndCredit
		}
		if r.Code == "6401" {
			cogsDr, cogsCr = r.EndDebit, r.EndCredit
		}
	}
	if revCr != "500.00" || revDr != "0.00" {
		t.Fatalf("反结账后 6001 应为贷 500，得到 借%s 贷%s", revDr, revCr)
	}
	if cogsDr != "200.00" || cogsCr != "0.00" {
		t.Fatalf("反结账后 6401 应为借 200，得到 借%s 贷%s", cogsDr, cogsCr)
	}
}

func TestReverseVoucher(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, _ := database.BeginTx(ctx, nil)
	defer tx.Rollback()
	v, err := CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-05-01",
		Summary:     "待冲销",
		Lines: []LineInput{
			{AccountCode: "1001", Debit: "50.00"},
			{AccountCode: "6301", Credit: "50.00"},
		},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, v.ID, "test"); err != nil {
		t.Fatal(err)
	}
	rev, err := ReverseVoucher(ctx, tx, v.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if rev.ReversesID == nil || *rev.ReversesID != v.ID {
		t.Fatal("reverse link missing")
	}
	book, err := AccountBook(ctx, tx, "1001", 2024, 5)
	if err != nil {
		t.Fatal(err)
	}
	if book.Closing != "0.00" && book.Closing != "0" {
		t.Fatalf("cash should net to 0 after reverse, got %s", book.Closing)
	}
}

func TestReverseVoucherConcurrent(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := CreateVoucher(ctx, tx, VoucherInput{
		VoucherDate: "2024-05-01",
		Summary:     "并发冲销",
		Lines: []LineInput{
			{AccountCode: "1001", Debit: "50.00"},
			{AccountCode: "6301", Credit: "50.00"},
		},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := PostVoucher(ctx, tx, v.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				errs[i] = err
				return
			}
			defer tx.Rollback()
			if _, err := ReverseVoucher(ctx, tx, v.ID, "test"); err != nil {
				errs[i] = err
				return
			}
			errs[i] = tx.Commit()
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("expected exactly 1 reverse to succeed, got %d (errs=%v)", ok, errs)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE reverses_id = $1`, v.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reversing voucher, got %d", n)
	}
}

func TestOpeningResave(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := SaveOpenings(ctx, tx, 2024, []LineInput{
		{AccountCode: "1002", Debit: "1000.00"},
		{AccountCode: "4001", Credit: "1000.00"},
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveOpenings(ctx, tx, 2024, []LineInput{
		{AccountCode: "1002", Debit: "2000.00"},
		{AccountCode: "4001", Credit: "2000.00"},
	}, "test"); err != nil {
		t.Fatal(err)
	}
	tb, err := TrialBalance(ctx, tx, 2024, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	var cashOpenDr, cashEndDr, capOpenCr, capEndCr string
	for _, r := range tb.Rows {
		if r.Code == "1002" {
			cashOpenDr, cashEndDr = r.OpeningDebit, r.EndDebit
		}
		if r.Code == "4001" {
			capOpenCr, capEndCr = r.OpeningCredit, r.EndCredit
		}
	}
	if cashOpenDr != "2000.00" || cashEndDr != "2000.00" {
		t.Fatalf("再次期初后 1002 应为期初/期末借 2000，得到 期初%s 期末%s", cashOpenDr, cashEndDr)
	}
	if capOpenCr != "2000.00" || capEndCr != "2000.00" {
		t.Fatalf("再次期初后 4001 应为期初/期末贷 2000，得到 期初%s 期末%s", capOpenCr, capEndCr)
	}
}

func TestBusinessEventIdempotent(t *testing.T) {
	// P0: 同一业务事件重复触发（重试/双发），只生成一张凭证
	database := testDB(t)
	ctx := context.Background()
	const soID = "00000000-0000-4000-8000-0000000000b1"
	const custID = "00000000-0000-4000-8000-0000000000c1"
	// 占位符用 $N：SQLite/PostgreSQL 双方言均可绑定（? 仅 SQLite 支持）。
	// ON CONFLICT DO NOTHING：固定 UUID + 共享库重跑时不因残留行撞主键。
	mustExec(t, ctx, database, `INSERT INTO customers (id, code, name) VALUES ($1, 'T-IDEM', '幂等客户') ON CONFLICT (id) DO NOTHING`, custID)
	mustExec(t, ctx, database, `INSERT INTO sales_orders (id, order_no, customer_id, status, total_amount) VALUES ($1, 'SO-IDEM-1', $2, 'confirmed', '100.00') ON CONFLICT (id) DO NOTHING`, soID, custID)

	run := func() error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := OnBusinessEvent(ctx, tx, "sales_order.confirmed", uuid.MustParse(soID), "test"); err != nil {
			return err
		}
		return tx.Commit()
	}

	if err := run(); err != nil {
		t.Fatalf("first post: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("second (idempotent) post: %v", err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE source_type='sales_order' AND source_id=$1`, soID).Scan(&n); err != nil {
		t.Fatalf("count vouchers: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 voucher, got %d", n)
	}
}
