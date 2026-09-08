package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nphq/starocean/internal/shared"
)

// Connect 根据 DSN 连接数据库:
//   - sqlite:<path>  → 单机 SQLite (CGO-free, modernc.org/sqlite)
//   - postgres://... → PostgreSQL (默认/可选)
func Connect(databaseURL string) (*sql.DB, error) {
	if isSQLiteDSN(databaseURL) {
		shared.SetSQLiteMode(true)
		database, err := OpenSQLite(databaseURL)
		if err != nil {
			return nil, err
		}
		log.Println("sqlite connected:", sqliteURI(databaseURL))
		return database, nil
	}

	shared.SetSQLiteMode(false)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	log.Println("database connected")
	return db, nil
}
