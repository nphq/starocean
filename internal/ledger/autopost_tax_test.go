package ledger

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// insertSalesOrder 组装一张含指定的含税单价/税率的销售订单（供 autopost 测试）。
func insertSalesOrder(t *testing.T, ctx context.Context, db DBTX, orderNo string, total string, qty int32, unitPrice string, taxRate string) (orderID, custID, prodID uuid.UUID) {
	t.Helper()
	custID, prodID, orderID = uuid.New(), uuid.New(), uuid.New()
	mustExec(t, ctx, db, `INSERT INTO customers (id, code, name) VALUES ($1,'C-tax','客户A')`, custID)
	mustExec(t, ctx, db, `INSERT INTO products (id, code, name, cost_price, current_stock) VALUES ($1,'P-tax','产品A',0,100)`, prodID)
	mustExec(t, ctx, db, `INSERT INTO sales_orders (id, order_no, customer_id, status, order_date, total_amount) VALUES ($1,$2,$3,'confirmed','2026-09-01',$4)`, orderID, orderNo, custID, total)
	mustExec(t, ctx, db, `INSERT INTO sales_order_items (id, order_id, product_id, quantity, unit_price, tax_rate) VALUES ($1,$2,$3,$4,$5,$6)`, uuid.New(), orderID, prodID, qty, unitPrice, taxRate)
	return
}

func mustExec(t *testing.T, ctx context.Context, db DBTX, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}

func voucherLinesBySource(t *testing.T, ctx context.Context, db DBTX, sourceType string, sourceID uuid.UUID) map[string][2]decimal.Decimal {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT l.account_code, l.debit, l.credit
		FROM gl_voucher_lines l JOIN gl_vouchers v ON v.id = l.voucher_id
		WHERE v.source_type=$1 AND v.source_id=$2`, sourceType, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][2]decimal.Decimal{}
	for rows.Next() {
		var code string
		var dr, cr decimal.Decimal
		if err := rows.Scan(&code, &dr, &cr); err != nil {
			t.Fatal(err)
		}
		out[code] = [2]decimal.Decimal{dr, cr}
	}
	return out
}

func TestSalesConfirmSplitsTax(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// 113 含税（13%）：净额 100，税额 13。
	orderID, _, _ := insertSalesOrder(t, ctx, tx, "SO-TAX-1", "113.00", 1, "113.00", "13.00")
	if err := OnBusinessEvent(ctx, tx, "sales_order.confirmed", orderID, "test"); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	lines := voucherLinesBySource(t, ctx, tx, "sales_order", orderID)
	// 借 AR 113
	if v, ok := lines["1122"]; !ok || !v[0].Equal(decimal.NewFromFloat(113)) || !v[1].IsZero() {
		t.Fatalf("AR line: expected debit 113, got %+v", lines["1122"])
	}
	// 贷收入 100
	if v, ok := lines["6001"]; !ok || !v[1].Equal(decimal.NewFromFloat(100)) {
		t.Fatalf("Revenue line: expected credit 100, got %+v", lines["6001"])
	}
	// 贷销项税 13
	if v, ok := lines["22210105"]; !ok || !v[1].Equal(decimal.NewFromFloat(13)) {
		t.Fatalf("Output tax line: expected credit 13, got %+v", lines["22210105"])
	}
}

func TestPurchaseReceiveSplitsTax(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	supID, prodID, orderID := uuid.New(), uuid.New(), uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO suppliers (id, code, name) VALUES ($1,'S-tax','供应商A')`, supID)
	mustExec(t, ctx, tx, `INSERT INTO products (id, code, name, cost_price, current_stock) VALUES ($1,'P-tax2','产品B',0,1)`, prodID)
	mustExec(t, ctx, tx, `INSERT INTO purchase_orders (id, order_no, supplier_id, status, order_date, total_amount) VALUES ($1,'PO-TAX-1',$2,'received','2026-09-01',113)`, orderID, supID)
	mustExec(t, ctx, tx, `INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price, tax_rate) VALUES ($1,$2,$3,1,113,13)`, uuid.New(), orderID, prodID)

	if err := OnBusinessEvent(ctx, tx, "purchase_order.received", orderID, "test"); err != nil {
		t.Fatalf("receive: %v", err)
	}

	lines := voucherLinesBySource(t, ctx, tx, "purchase_order", orderID)
	// 借存货 100
	if v, ok := lines["1405"]; !ok || !v[0].Equal(decimal.NewFromFloat(100)) {
		t.Fatalf("Inventory line: expected debit 100, got %+v", lines["1405"])
	}
	// 贷应付 113
	if v, ok := lines["2202"]; !ok || !v[1].Equal(decimal.NewFromFloat(113)) {
		t.Fatalf("AP line: expected credit 113, got %+v", lines["2202"])
	}
	// 借进项税 13
	if v, ok := lines["22210101"]; !ok || !v[0].Equal(decimal.NewFromFloat(13)) {
		t.Fatalf("Input tax line: expected debit 13, got %+v", lines["22210101"])
	}
	// 移动加权按净额入成本（进项不计入）：0 库存时新成本 = 净额 100，而非含税 113。
	var cost decimal.Decimal
	tx.QueryRowContext(ctx, `SELECT COALESCE(cost_price,0) FROM products WHERE id=$1`, prodID).Scan(&cost)
	if !cost.Equal(decimal.NewFromFloat(100)) {
		t.Fatalf("expected moving-avg cost 100 (net), got %s", cost.String())
	}
}

func TestMovingAverageNetSymmetric(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// 采购前：库存 5、成本 80（现有价值 400）。采购 1 件、含税 113、13% → 净额 100。
	prodID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO products (id, code, name, cost_price, current_stock) VALUES ($1,'P-avg','产品C',80,6)`, prodID)
	supID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO suppliers (id, code, name) VALUES ($1,'S-avg','供应商B')`, supID)
	purchaseID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO purchase_orders (id, order_no, supplier_id, status, order_date, total_amount) VALUES ($1,'PO-AVG-1',$2,'received','2026-09-01',113)`, purchaseID, supID)
	mustExec(t, ctx, tx, `INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price, tax_rate) VALUES ($1,$2,$3,1,113,13)`, uuid.New(), purchaseID, prodID)

	// 入库后（库存 6）用净额更新成本：(5*80 + 100)/6 = 83.33（若用含税则 85.50）
	if err := updateMovingAverage(ctx, tx, purchaseID); err != nil {
		t.Fatalf("updateMovingAverage: %v", err)
	}
	var after decimal.Decimal
	tx.QueryRowContext(ctx, `SELECT COALESCE(cost_price,0) FROM products WHERE id=$1`, prodID).Scan(&after)
	if !after.Equal(decimal.NewFromFloat(83.33)) {
		t.Fatalf("expected cost 83.33 (net), got %s", after.String())
	}

	// 红冲回退：库存回到 5，成对称地恢复原成本 80.00。
	mustExec(t, ctx, tx, `UPDATE products SET current_stock=5 WHERE id=$1`, prodID)
	if err := revertMovingAverage(ctx, tx, purchaseID); err != nil {
		t.Fatalf("revertMovingAverage: %v", err)
	}
	var reverted decimal.Decimal
	tx.QueryRowContext(ctx, `SELECT COALESCE(cost_price,0) FROM products WHERE id=$1`, prodID).Scan(&reverted)
	if !reverted.Equal(decimal.NewFromFloat(80.00)) {
		t.Fatalf("expected cost recovered to 80.00, got %s", reverted.String())
	}
}

func TestZeroTaxLineOmitted(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// 税率 0：分录退化为现状两行式，不产生 0 税行。
	orderID, _, _ := insertSalesOrder(t, ctx, tx, "SO-ZERO-1", "100.00", 1, "100.00", "0")
	if err := OnBusinessEvent(ctx, tx, "sales_order.confirmed", orderID, "test"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	lines := voucherLinesBySource(t, ctx, tx, "sales_order", orderID)
	if v, ok := lines["6001"]; !ok || !v[1].Equal(decimal.NewFromFloat(100)) {
		t.Fatalf("Revenue line: expected credit 100, got %+v", lines["6001"])
	}
	if _, ok := lines["22210105"]; ok {
		t.Fatalf("expected no output-tax line for 0 rate, got %+v", lines["22210105"])
	}
}

// 同单多行同商品：净额逐行拆分后必须在商品维度聚合；收货/取消成本对称回退。
// 回归：旧版 revertMovingAverage 的 GROUP BY 裸列在 PG 报 42803，SQLite 下取任意行导致回退错误。
func TestMovingAverageMultiLineSameProduct(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// 采购前：库存 10、成本 50。收货后库存 13。同单两行同商品：
	//   行1: 2件 × 56.50（含税113，13%）→ 净额 100
	//   行2: 1件 × 10.60（含税10.60，6%）→ 净额 10
	prodID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO products (id, code, name, cost_price, current_stock) VALUES ($1,'P-multi','产品D',50,13)`, prodID)
	supID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO suppliers (id, code, name) VALUES ($1,'S-multi','供应商C')`, supID)
	purchaseID := uuid.New()
	mustExec(t, ctx, tx, `INSERT INTO purchase_orders (id, order_no, supplier_id, status, order_date, total_amount) VALUES ($1,'PO-MULTI-1',$2,'received','2026-09-01',123.60)`, purchaseID, supID)
	mustExec(t, ctx, tx, `INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price, tax_rate) VALUES ($1,$2,$3,2,56.50,13)`, uuid.New(), purchaseID, prodID)
	mustExec(t, ctx, tx, `INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price, tax_rate) VALUES ($1,$2,$3,1,10.60,6)`, uuid.New(), purchaseID, prodID)

	if err := updateMovingAverage(ctx, tx, purchaseID); err != nil {
		t.Fatalf("updateMovingAverage: %v", err)
	}
	var after decimal.Decimal
	tx.QueryRowContext(ctx, `SELECT COALESCE(cost_price,0) FROM products WHERE id=$1`, prodID).Scan(&after)
	// (10*50 + 100 + 10)/13 = 46.923 → 46.92（漏聚合净额或误用含税均不等于该值）
	if !after.Equal(decimal.NewFromFloat(46.92)) {
		t.Fatalf("expected cost 46.92, got %s", after.String())
	}

	mustExec(t, ctx, tx, `UPDATE products SET current_stock=10 WHERE id=$1`, prodID)
	if err := revertMovingAverage(ctx, tx, purchaseID); err != nil {
		t.Fatalf("revertMovingAverage: %v", err)
	}
	var reverted decimal.Decimal
	tx.QueryRowContext(ctx, `SELECT COALESCE(cost_price,0) FROM products WHERE id=$1`, prodID).Scan(&reverted)
	if !reverted.Equal(decimal.NewFromFloat(50.00)) {
		t.Fatalf("expected cost recovered to 50.00, got %s", reverted.String())
	}
}
