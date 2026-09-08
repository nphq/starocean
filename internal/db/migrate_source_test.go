package db

import (
	"testing"
	"testing/fstest"
)

// 回归测试：CI postgres 任务曾让 internal/ledger 全灭：
// create iofs source: failed to init driver with path internal/db/migrations:
// open internal/db/migrations: file does not exist。
//
// 根因：Migrate 的 PG 分支硬编码 internal/db/migrations（仅适配 main.go 的
// embed 根），而 ledger 测试传入的 db.EmbeddedMigrations 根为 internal/db，
// 路径应为 migrations。本地默认 SQLite 走 MigrateSQLite（自带双根兼容），
// 所以本地全绿、CI postgres 必红。本测试不依赖任何数据库。
func TestNewMigrationSourceBothRoots(t *testing.T) {
	// 仓库根风格的 FS（main.go 的 migrationsFS）：第一分支命中。
	repoRoot := fstest.MapFS{
		"internal/db/migrations/001_init.up.sql":   {Data: []byte("-- up")},
		"internal/db/migrations/001_init.down.sql": {Data: []byte("-- down")},
	}
	d, err := newMigrationSource(repoRoot)
	if err != nil {
		t.Fatalf("repo-root FS: %v", err)
	}
	if v, err := d.First(); err != nil || v != 1 {
		t.Fatalf("repo-root FS: First() = %d, %v", v, err)
	}

	// db 包内嵌（根为 internal/db）：必须回退到第二分支，此前这里直接报错。
	d, err = newMigrationSource(EmbeddedMigrations)
	if err != nil {
		t.Fatalf("db embed FS: %v", err)
	}
	v, err := d.First()
	if err != nil || v <= 0 {
		t.Fatalf("db embed FS: First() = %d, %v", v, err)
	}
	if rc, _, err := d.ReadUp(uint(v)); err != nil || rc == nil {
		t.Fatalf("db embed FS: ReadUp(%d) err=%v", v, err)
	} else {
		rc.Close()
	}
}
