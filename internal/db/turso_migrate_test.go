package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateTurso(t *testing.T) {
	dsn := os.Getenv("STAROCEAN_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("SQLITE_TEST_DSN")
	}
	if dsn == "" {
		// 缺省用临时库直接跑：无 DSN 时 Skip 会造成假绿
		dsn = "sqlite:" + filepath.Join(t.TempDir(), "starocean-test.db")
	}
	if !strings.HasPrefix(dsn, "sqlite:") && !strings.HasPrefix(dsn, "turso:") {
		t.Fatalf("not a turso DSN: %q", dsn)
	}
	// go test ./... 各包并行运行，使用独立文件避免相互冲突
	p := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(dsn, "sqlite:"), "turso:"), ".db") + "_db.db"
	dsn = "sqlite:" + p
	database, err := OpenTurso(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := MigrateTurso(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 幂等: 再次执行应无变化、无报错
	if err := MigrateTurso(ctx, database); err != nil {
		t.Fatalf("migrate idempotent: %v", err)
	}

	// 关键表存在
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('sales_orders','gl_vouchers','inventory_batches','finance_clearings','gl_periods')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("expected 5 key tables, got %d", n)
	}
	// 触发器齐全（计算列语义载体）
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'trg\_%\_gen\_%' ESCAPE '\'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Errorf("expected 10 generated-column triggers, got %d", n)
	}
	// 种子：60 期 + 44 科目
	if err := database.QueryRow(`SELECT COUNT(*) FROM gl_periods`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 60 {
		t.Errorf("expected 60 periods, got %d", n)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gl_accounts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 44 {
		t.Errorf("expected 44 accounts, got %d", n)
	}
	// 触发器真实生效：建单据行自动算 amount
	if _, err := database.Exec(`INSERT INTO sales_orders (id, order_no) VALUES ('t-ord-1', 'T-001')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO products (id, code, name) VALUES ('t-prod-1', 'T-P1', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sales_order_items (id, order_id, product_id, quantity, unit_price) VALUES ('t-item-1', 't-ord-1', 't-prod-1', 3, '10.00')`); err != nil {
		t.Fatal(err)
	}
	var amt string
	if err := database.QueryRow(`SELECT CAST(amount AS TEXT) FROM sales_order_items WHERE id='t-item-1'`).Scan(&amt); err != nil {
		t.Fatal(err)
	}
	if amt != "30" {
		t.Errorf("trigger amount: expected 30, got %q", amt)
	}
}
