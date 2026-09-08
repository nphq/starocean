package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
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

	d, err := iofs.New(fs, "internal/db/migrations")
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
