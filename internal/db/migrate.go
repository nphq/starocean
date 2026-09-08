package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrate 自动执行数据库迁移:
//   - SQLite:  使用内嵌迁移文件 + DDL 翻译 (见 sqlite_migrate.go)
//   - Postgres: 使用 golang-migrate
func Migrate(database *sql.DB, databaseURL string, fs embed.FS) error {
	if isSQLiteDSN(databaseURL) {
		if err := MigrateSQLite(context.Background(), database, fs); err != nil {
			return fmt.Errorf("run sqlite migrations: %w", err)
		}
		log.Println("sqlite migrations done")
		return nil
	}

	d, err := newMigrationSource(fs)
	if err != nil {
		return fmt.Errorf("create iofs source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// newMigrationSource 构造 golang-migrate 的 iofs 源，兼容两种 embed 根：
//   - main.go 内嵌（根为仓库根）：internal/db/migrations
//   - db 包内 EmbeddedMigrations（根为 internal/db）：migrations
//
// MigrateSQLite 内已有同款双根兼容；PG 分支此前只写了第一种，导致传入
// db.EmbeddedMigrations 时（internal/ledger 测试）直接报错
// "create iofs source: ... open internal/db/migrations: file does not exist"。
func newMigrationSource(fsys fs.FS) (source.Driver, error) {
	if d, err := iofs.New(fsys, "internal/db/migrations"); err == nil {
		return d, nil
	}
	d, err := iofs.New(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("tried internal/db/migrations and migrations: %w", err)
	}
	return d, nil
}
