package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// 基线 schema 执行器：internal/db/schema.sql（Turso 方言，直接可执行）。
//
// 开发阶段无历史数据兼容负担：全库由单一 schema 文件一次建出，不做版本化迁移。
// 幂等：表/触发器/索引一律 IF NOT EXISTS，种子行一律 ON CONFLICT DO NOTHING，
// Migrate 可重复执行。改表结构直接改 schema.sql（本地库删文件重建）。
// ---------------------------------------------------------------------------

// MigrateTurso 执行内嵌基线 schema（幂等，可重复调用）。
func MigrateTurso(ctx context.Context, database *sql.DB) error {
	content, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema.sql: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, stmt := range splitStatements(string(content)) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("schema: %w\nstatement: %s", err, truncate(stmt, 160))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema: %w", err)
	}
	return nil
}

// splitStatements 按分号切分 DDL(纯 DDL/INSERT，字符串字面量中不含分号)，
// 并先移除整行注释，避免"以注释开头"的语句被丢弃。
// schema.sql 含 CREATE TRIGGER ... BEGIN ...; END 语句，其体内的分号
// 不能切分：遇到 CREATE TRIGGER 即累积直到以 END 收尾的片段。
var (
	reTriggerStart = regexp.MustCompile(`(?i)CREATE\s+TRIGGER\b`)
	reTriggerEnd   = regexp.MustCompile(`(?i)\bEND\s*$`)
)

func splitStatements(ddl string) []string {
	var lines []string
	for _, l := range strings.Split(ddl, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			continue
		}
		lines = append(lines, l)
	}
	var out []string
	var (
		buf       string
		inTrigger bool
	)
	flush := func() {
		if strings.TrimSpace(buf) != "" {
			out = append(out, strings.TrimSpace(buf))
		}
		buf = ""
		inTrigger = false
	}
	for _, part := range strings.Split(strings.Join(lines, "\n"), ";") {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		if inTrigger {
			buf += ";\n" + stmt
			if reTriggerEnd.MatchString(stmt) {
				flush()
			}
			continue
		}
		if reTriggerStart.MatchString(stmt) {
			if reTriggerEnd.MatchString(stmt) {
				out = append(out, stmt)
				continue
			}
			buf = stmt
			inTrigger = true
			continue
		}
		out = append(out, stmt)
	}
	flush()
	return out
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
