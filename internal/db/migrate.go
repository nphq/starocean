package db

import (
	"context"
	"database/sql"
	"log"
)

// Migrate 执行内嵌基线 schema（幂等，见 turso_migrate.go）。
func Migrate(database *sql.DB) error {
	if err := MigrateTurso(context.Background(), database); err != nil {
		return err
	}
	log.Println("turso schema done")
	return nil
}
