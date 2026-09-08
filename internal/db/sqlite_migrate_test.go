package db

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed migrations
var testMigrationsFS embed.FS

func TestTranslateDDL(t *testing.T) {
	content, err := testMigrationsFS.ReadFile("migrations/001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	out := translateDDL(string(content))
	if os.Getenv("SHOW_DDL") == "1" {
		t.Log("\n" + out)
	}
	// 基础断言: 类型/默认值翻译
	for _, want := range []string{`TEXT PRIMARY KEY`, `NUMERIC`, `DEFAULT CURRENT_TIMESTAMP`} {
		if !strings.Contains(out, want) {
			t.Errorf("translated DDL missing %q", want)
		}
	}
	if strings.Contains(out, "gen_random_uuid()") || strings.Contains(out, "UUID") || strings.Contains(out, "TIMESTAMPTZ") {
		os.WriteFile("/tmp/bad_ddl.sql", []byte(out), 0o644)
		t.Errorf("translated DDL still contains PG types, wrote /tmp/bad_ddl.sql")
	}
}

func TestMigrateSQLite(t *testing.T) {
	dsn := os.Getenv("STAROCEAN_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("SQLITE_TEST_DSN")
	}
	if dsn == "" {
		// 缺省用临时 SQLite 库直接跑：无 DSN 时 Skip 会造成假绿
		dsn = "sqlite:" + filepath.Join(t.TempDir(), "starocean-test.db")
	}
	if !strings.HasPrefix(dsn, "sqlite:") {
		t.Skip("not a sqlite DSN (sqlite-only test)")
	}
	// go test ./... 各包并行运行，SQLite 使用独立文件避免相互冲突
	p := strings.TrimSuffix(strings.TrimPrefix(dsn, "sqlite:"), ".db") + "_db.db"
	dsn = "sqlite:" + p
	db, err := OpenSQLite(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := MigrateSQLite(context.Background(), db, testMigrationsFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 幂等: 迁移再次运行应无变化
	if err := MigrateSQLite(context.Background(), db, testMigrationsFS); err != nil {
		t.Fatalf("migrate idempotent: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('sales_orders','plugins','gl_vouchers','custom_fields','inventory_batches')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("expected 5 key tables, got %d", n)
	}
}
