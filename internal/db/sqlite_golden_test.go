package db

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Golden 对照测试: 锁死 PG→SQLite 翻译层 (rewriteQuery / translateDDL / splitStatements)。
// 纯函数测试，无 DB 依赖；任何翻译规则变更必须先经此处。
// ---------------------------------------------------------------------------

func TestRewriteQueryGolden(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cast_text", `SELECT properties::text FROM t`, `SELECT CAST(properties AS TEXT) FROM t`},
		{"cast_qualified", `SELECT so.properties::text FROM sales_orders so`, `SELECT CAST(so.properties AS TEXT) FROM sales_orders so`},
		{"cast_numeric_func", `SELECT COALESCE(so.total_amount, 0)::numeric FROM t`, `SELECT CAST(COALESCE(so.total_amount, 0) AS NUMERIC) FROM t`},
		{"cast_inside_coalesce", `SELECT COALESCE(properties::text, '{}') FROM t`, `SELECT COALESCE(CAST(properties AS TEXT), '{}') FROM t`},
		{"cast_timestamptz_literal", `SELECT COALESCE(created_at, '1970-01-01'::timestamptz) FROM t`, `SELECT COALESCE(created_at, CAST('1970-01-01' AS TEXT)) FROM t`},
		{"cast_param_jsonb", `UPDATE t SET x = $2::jsonb WHERE id = $1`, `UPDATE t SET x = CAST($2 AS TEXT) WHERE id = $1`},
		{"ilike", `WHERE name ILIKE '%'||$1||'%' OR code ILIKE '%'||$1||'%'`, `WHERE name LIKE '%'||$1||'%' OR code LIKE '%'||$1||'%'`},
		{"now", `UPDATE t SET updated_at = NOW() WHERE id = $1`, `UPDATE t SET updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`},
		{"for_update_stripped", `SELECT id FROM sales_orders WHERE id = $1 FOR UPDATE`, `SELECT id FROM sales_orders WHERE id = $1`},
		{"gen_random_uuid", `INSERT INTO t (id) VALUES (gen_random_uuid())`, `INSERT INTO t (id) VALUES ((lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6)))))`},
		{"interval_column", `AND order_date < d.month + INTERVAL '1 month'`, `AND order_date < date(d.month, '+1 month')`},
		{"interval_current_date", `end_date <= CURRENT_DATE + INTERVAL '30 days'`, `end_date <= date(CURRENT_DATE, '+30 days')`},
		{"date_trunc_month", `WHERE order_date >= DATE_TRUNC('month', CURRENT_DATE)::date`, `WHERE order_date >= date('now','start of month')`},
		{"date_trunc_month_interval", `month >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '11 months'`, `month >= date('now','start of month','-11 months')`},
		{"to_char", `TO_CHAR(month, 'YYYY-MM') = $1`, `strftime('%Y-%m', month) = $1`},
		{"jsonb_concat", `UPDATE t SET properties = properties || $2::jsonb, updated_at = NOW() WHERE id = $1`,
			`UPDATE t SET properties = json_patch(properties, $2), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`},
		{"time_cast", `check_in_time::time > '09:00'`, `time(check_in_time) > '09:00'`},
		{"nulls_last_in_select_kept", `ORDER BY created_at DESC NULLS LAST, id DESC`, `ORDER BY created_at DESC NULLS LAST, id DESC`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteQuery(tc.in)
			if got != tc.want {
				t.Errorf("rewrite mismatch:\n in:  %s\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestTranslateDDLGolden(t *testing.T) {
	ddl := `-- 主数据
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(100) NOT NULL UNIQUE,
    name TEXT NOT NULL,
    sale_price DECIMAL(15,2) DEFAULT 0,
    tags TEXT[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    meta JSONB NOT NULL DEFAULT '{}',
    expire_date DATE,
    sort_idx SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_products_name_gist ON products USING gist (name gist_trgm_ops);
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE SEQUENCE IF NOT EXISTS seq_name;
ALTER TABLE products ADD COLUMN IF NOT EXISTS extra_note TEXT;`
	got := translateDDL(ddl)

	for _, forbidden := range []string{"UUID", "VARCHAR", "DECIMAL", "BOOLEAN", "JSONB", "TEXT[]", "SMALLINT", "TIMESTAMPTZ",
		"gen_random_uuid()", "USING gist", "CREATE EXTENSION", "CREATE SEQUENCE", "IF NOT EXISTS extra_note"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("translated DDL still contains %q:\n%s", forbidden, got)
		}
	}
	for _, want := range []string{"id TEXT PRIMARY KEY", "DEFAULT CURRENT_TIMESTAMP", "INTEGER", "NUMERIC", "expire_date TEXT"} {
		if !strings.Contains(got, want) {
			t.Errorf("translated DDL missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ADD COLUMN extra_note") && strings.Contains(got, "IF NOT EXISTS") {
		t.Errorf("ADD COLUMN IF NOT EXISTS not stripped:\n%s", got)
	}
}

func TestTranslateDDLGLSeedGolden(t *testing.T) {
	seed := `INSERT INTO gl_periods (year, month, start_date, end_date, status)
SELECT y, m, make_date(y, m, 1),
       (make_date(y, m, 1) + INTERVAL '1 month' - INTERVAL '1 day')::date,
       'open'
FROM generate_series(2024, 2028) AS y, generate_series(1, 12) AS m;`
	got := translateDDL(seed)
	if strings.Contains(got, "make_date") || strings.Contains(got, "generate_series") {
		t.Fatalf("GL seed not replaced:\n%s", got)
	}
	for _, want := range []string{"WITH RECURSIVE years", "date(y || '-' || printf('%02d', m) || '-01')"} {
		if !strings.Contains(got, want) {
			t.Errorf("GL seed replacement missing %q:\n%s", want, got)
		}
	}
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

func TestDateFunctionNotMangled(t *testing.T) {
	// 回归: translateDDL 把 date(...) 函数调用误替换为列类型 (GL seed 修复)
	ddl := `SELECT date('now','start of month') AS m;`
	got := translateDDL(ddl)
	if !strings.Contains(got, "date('now','start of month')") {
		t.Fatalf("date() function mangled: %s", got)
	}
}
