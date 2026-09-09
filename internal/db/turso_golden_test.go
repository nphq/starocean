package db

import (
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Golden 对照测试: 锁死查询层 (renumberPlaceholders / splitStatements) 与
// 基线 schema (schema.sql)。业务 SQL 已为 Turso 原生方言（仅保留 $N 占位符，
// 由驱动层按出现顺序重排绑定）；任何占位符规则或 schema 变更必须先经此处。
// ---------------------------------------------------------------------------

func TestRenumberPlaceholdersGolden(t *testing.T) {
	// 乱序编号 → 按出现顺序改写为 ? + 映射
	got, perm := renumberPlaceholders(`UPDATE t SET a=$2 WHERE id=$1`)
	if got != `UPDATE t SET a=? WHERE id=?` {
		t.Fatalf("rewrite: %q", got)
	}
	if strings.Join(intsToStrings(perm), ",") != "2,1" {
		t.Fatalf("perm: %v", perm)
	}
	// 重复编号 → 槽位重复映射
	got, perm = renumberPlaceholders(`SET a=$4, b=$5, c=$4 WHERE x=$1 AND y=$2 AND z=$3`)
	if strings.Count(got, "?") != 6 {
		t.Fatalf("slots: %q", got)
	}
	if strings.Join(intsToStrings(perm), ",") != "4,5,4,1,2,3" {
		t.Fatalf("perm: %v", perm)
	}
	// 纯 ? 查询 → 恒等快路径
	got, perm = renumberPlaceholders(`WHERE d >= ? AND d < ?`)
	if got != `WHERE d >= ? AND d < ?` || perm != nil {
		t.Fatalf("identity: %q %v", got, perm)
	}
	// 字符串字面量内的 $N 不处理
	got, perm = renumberPlaceholders(`SELECT '$1' AS x WHERE id=$1`)
	if got != `SELECT '$1' AS x WHERE id=?` {
		t.Fatalf("literal: %q", got)
	}
	if strings.Join(intsToStrings(perm), ",") != "1" {
		t.Fatalf("perm: %v", perm)
	}
}

func intsToStrings(ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = strconv.Itoa(n)
	}
	return out
}

func TestSplitStatementsKeepsCommentedLayout(t *testing.T) {
	// 回归: 以注释开头导致整条语句被丢弃 (早期 bug)
	ddl := `-- Users (single-user auth)
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE
);
-- Customers
CREATE TABLE customers (
    id TEXT PRIMARY KEY
);`
	stmts := splitStatements(ddl)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d: %+v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "CREATE TABLE users") || !strings.Contains(stmts[1], "CREATE TABLE customers") {
		t.Fatalf("statements lost: %+v", stmts)
	}
}

func TestSplitStatementsKeepsTriggerBody(t *testing.T) {
	// 触发器体内的分号不能切分语句
	ddl := `CREATE TABLE t (id TEXT PRIMARY KEY, a NUMERIC);
CREATE TRIGGER trg_ai AFTER INSERT ON t BEGIN UPDATE t SET a = (1 + 2) WHERE rowid = NEW.rowid; END;
CREATE INDEX idx ON t(id);`
	stmts := splitStatements(ddl)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %+v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[1], "CREATE TRIGGER") || !strings.Contains(stmts[1], "END") {
		t.Fatalf("trigger split in the middle: %q", stmts[1])
	}
}

// TestSchemaGolden 锁死基线 schema 的形状：Turbo 方言、无 PG 残留、幂等子句齐全。
func TestSchemaGolden(t *testing.T) {
	raw, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)

	// 27 张表齐全
	tables := []string{
		"users", "customers", "suppliers", "products",
		"sales_orders", "sales_order_items", "purchase_orders", "purchase_order_items",
		"order_sequences", "inventory_movements", "inventory_batches",
		"payments", "finance_clearings",
		"reimbursements", "reimbursement_items", "invoices",
		"reconciliations", "reconciliation_items",
		"product_price_tiers", "customer_product_prices",
		"gl_settings", "gl_accounts", "gl_periods", "gl_voucher_seq",
		"gl_vouchers", "gl_voucher_lines", "gl_account_balances",
	}
	for _, tb := range tables {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+tb+" ") &&
			!strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+tb+"(") {
			t.Errorf("schema missing table %q", tb)
		}
	}

	// 10 个计算列触发器齐全
	triggers := []string{
		"trg_sales_order_items_amount_gen_ai", "trg_sales_order_items_amount_gen_au",
		"trg_purchase_order_items_amount_gen_ai", "trg_purchase_order_items_amount_gen_au",
		"trg_invoices_total_amount_gen_ai", "trg_invoices_total_amount_gen_au",
		"trg_products_is_low_stock_gen_ai", "trg_products_is_low_stock_gen_au",
		"trg_products_search_text_gen_ai", "trg_products_search_text_gen_au",
	}
	for _, trg := range triggers {
		if !strings.Contains(schema, "CREATE TRIGGER IF NOT EXISTS "+trg+" ") {
			t.Errorf("schema missing trigger %q", trg)
		}
	}

	// 关键索引（含部分唯一索引）
	for _, want := range []string{
		"idx_gl_vouchers_source_unique",
		"idx_payments_client_token",
		"idx_clearings_payment_doc",
		"idx_products_low_stock",
		"idx_batches_expiry",
		"idx_customer_price_lookup",
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema missing index %q", want)
		}
	}

	// 种子：60 期 + 44 科目，且幂等（元组以 ", 'open')" 收尾，避开列定义中的 'open'）
	if n := strings.Count(schema, ", 'open')"); n != 60 {
		t.Errorf("gl_periods seed should have 60 rows, got %d", n)
	}
	for _, want := range []string{
		"('1001', '库存现金'", "('6801', '所得税费用'",
		"ON CONFLICT (company_id) DO NOTHING",
		"ON CONFLICT (code) DO NOTHING",
		"ON CONFLICT (year, month, company_id) DO NOTHING",
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema seed missing %q", want)
		}
	}

	// PG 方言零残留（schema 必须 Turso 原生可执行，无需翻译）
	for _, forbidden := range []string{
		"GENERATED ALWAYS AS", "gen_random_uuid()", "TIMESTAMPTZ", "VARCHAR(",
		"TEXT[]", "SMALLINT", "JSONB", "make_date", "generate_series",
		"WITH RECURSIVE", "USING gin", "USING gist", "CREATE EXTENSION", "CREATE SEQUENCE",
	} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("schema still contains PG-ism %q", forbidden)
		}
	}
	if strings.Contains(schema, "::") {
		t.Errorf("schema still contains :: casts")
	}
}
