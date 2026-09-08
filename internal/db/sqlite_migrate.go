package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// SQLite 迁移: 把 PostgreSQL 迁移 DDL 翻译为 SQLite DDL 并执行。
//
// 与 PostgreSQL (golang-migrate) 不同，这里不引入额外的迁移库:
// 直接读取 embed 的 internal/db/migrations/*.up.sql，翻译后逐条执行，
// 并以 schema_migrations 表记录版本号。
// ---------------------------------------------------------------------------

func MigrateSQLite(ctx context.Context, database *sql.DB, fsys fs.FS) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		dirty INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, database)
	if err != nil {
		return err
	}

	// 兼容两种 embed 根: internal/db/migrations (main.go) 与 migrations (包内测试)
	migroot := "internal/db/migrations"
	if _, err := fs.Stat(fsys, migroot); err != nil {
		if _, err2 := fs.Stat(fsys, "migrations"); err2 != nil {
			return fmt.Errorf("read migrations dir: %w", err)
		}
		migroot = "migrations"
	}

	entries, err := fs.ReadDir(fsys, migroot)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	type mig struct {
		version int
		name    string
	}
	var migrations []mig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, ok := parseVersion(e.Name())
		if !ok {
			continue
		}
		migrations = append(migrations, mig{version: v, name: e.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		content, err := fs.ReadFile(fsys, migroot+"/"+m.name)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.name, err)
		}
		ddl := translateDDL(string(content))
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(ddl) {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w\nstatement: %s", m.name, err, truncate(stmt, 160))
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES ($1, 0)`, m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, database *sql.DB) (map[int]bool, error) {
	rows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func parseVersion(name string) (int, bool) {
	idx := strings.IndexByte(name, '_')
	if idx <= 0 {
		return 0, false
	}
	var v int
	for _, c := range name[:idx] {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

// splitStatements 按分号切分 DDL(迁移文件是纯 DDL/INSERT，字符串字面量中不含分号)，
// 并先移除整行注释，避免"以注释开头"的语句被丢弃。
func splitStatements(ddl string) []string {
	var lines []string
	for _, l := range strings.Split(ddl, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			continue
		}
		lines = append(lines, l)
	}
	var out []string
	for _, part := range strings.Split(strings.Join(lines, "\n"), ";") {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// ---------------------------------------------------------------------------
// PostgreSQL DDL → SQLite DDL
// ---------------------------------------------------------------------------

var (
	ddlUUID       = regexp.MustCompile(`(?i)\bUUID\b`)
	ddlTimestamp  = regexp.MustCompile(`(?i)\bTIMESTAMPTZ\b`)
	ddlVarchar    = regexp.MustCompile(`(?i)\b(VARCHAR|CHAR|CHARACTER VARYING|NVARCHAR)\((\d+)\)`)
	ddlTextArray  = regexp.MustCompile(`(?i)\bTEXT\[\]`)
	ddlDecimal    = regexp.MustCompile(`(?i)\b(DECIMAL|NUMERIC|MONEY)\(\d+,\s*\d+\)`)
	ddlNumeric    = regexp.MustCompile(`(?i)\b(NUMERIC|DECIMAL)\b`)
	ddlBoolean    = regexp.MustCompile(`(?i)\bBOOLEAN\b`)
	ddlJSONB      = regexp.MustCompile(`(?i)\bJSONB\b`)
	ddlSmallInt   = regexp.MustCompile(`(?i)\bSMALLINT\b`)
	ddlNullsOrder = regexp.MustCompile(`(?i)\s+NULLS\s+(LAST|FIRST)`)
	ddlDefaultG   = regexp.MustCompile(`(?i)DEFAULT\s+gen_random_uuid\(\)`)
	ddlDefaultNow = regexp.MustCompile(`(?i)DEFAULT\s+NOW\(\)`)
	ddlGinIndex   = regexp.MustCompile(`(?is)CREATE\s+(UNIQUE\s+)?INDEX[^;]*USING\s+(gin|gist|hash|brin)[^;]*;`)
	ddlExtension  = regexp.MustCompile(`(?is)CREATE\s+EXTENSION[^;]*;`)
	ddlSequence   = regexp.MustCompile(`(?is)CREATE\s+SEQUENCE[^;]*;`)
	ddlAddColInex = regexp.MustCompile(`(?i)\bADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\b`)
	// 023_gl: PG 专属的 make_date + generate_series 种子语句 → SQLite 递归 CTE。
	// 注意: 必须在类型翻译(::date→CAST)之前替换。
	ddlGLSeed = regexp.MustCompile(`(?is)INSERT\s+INTO\s+gl_periods\s*\(year,\s*month,\s*start_date,\s*end_date,\s*status\)\s*SELECT\s+y,\s*m,\s*make_date\(y,\s*m,\s*1\),\s*\(make_date\(y,\s*m,\s*1\)\s*\+\s*INTERVAL\s*'1\s*month'\s*-\s*INTERVAL\s*'1\s*day'\)::date,\s*'open'\s*FROM\s+generate_series\(2024,\s*2028\)\s*AS\s+y,\s*generate_series\(1,\s*12\)\s*AS\s*m;`)
)

const ddlGLSeedReplacement = `INSERT INTO gl_periods (year, month, start_date, end_date, status)
SELECT y, m, date(y || '-' || printf('%02d', m) || '-01'),
       date(y || '-' || printf('%02d', m) || '-01', '+1 month', '-1 day'), 'open'
FROM (
  WITH RECURSIVE years(y) AS (SELECT 2024 UNION ALL SELECT y + 1 FROM years WHERE y < 2028),
       months(m) AS (SELECT 1 UNION ALL SELECT m + 1 FROM months WHERE m < 12)
  SELECT y, m FROM years, months
);`

// ddlDateCol 匹配列类型 DATE（不匹配函数调用 date(...)）
var ddlDateCol = regexp.MustCompile(`(?i)\bDATE\b`)

// replaceDateTypes 只把"列类型"位置的 DATE 替换为 TEXT，
// 跳过 date(...) 函数调用（如 GL 种子语句中的递归 CTE 表达式）。
func replaceDateTypes(ddl string) string {
	var b strings.Builder
	last := 0
	for _, idx := range ddlDateCol.FindAllStringIndex(ddl, -1) {
		tail := strings.TrimLeft(ddl[idx[1]:], " \t")
		if strings.HasPrefix(tail, "(") {
			// 函数调用形式，保留原样
			b.WriteString(ddl[last:idx[1]])
		} else {
			b.WriteString(ddl[last:idx[0]])
			b.WriteString("TEXT")
		}
		last = idx[1]
	}
	b.WriteString(ddl[last:])
	return b.String()
}

// translateDDL 将 PostgreSQL DDL 翻译为 SQLite 可执行 DDL。
func translateDDL(ddl string) string {
	ddl = ddlGLSeed.ReplaceAllString(ddl, ddlGLSeedReplacement)
	ddl = ddlGinIndex.ReplaceAllString(ddl, "")
	ddl = ddlExtension.ReplaceAllString(ddl, "")
	ddl = ddlSequence.ReplaceAllString(ddl, "")
	// SQLite 的 ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS（版本号已保证不重复执行）
	ddl = ddlAddColInex.ReplaceAllString(ddl, "ADD COLUMN")
	ddl = ddlTimestamp.ReplaceAllString(ddl, "TEXT")
	ddl = ddlVarchar.ReplaceAllString(ddl, "TEXT")
	ddl = ddlTextArray.ReplaceAllString(ddl, "TEXT")
	ddl = ddlDecimal.ReplaceAllString(ddl, "NUMERIC")
	ddl = ddlNumeric.ReplaceAllString(ddl, "NUMERIC")
	ddl = ddlBoolean.ReplaceAllString(ddl, "INTEGER")
	ddl = ddlJSONB.ReplaceAllString(ddl, "TEXT")
	ddl = replaceDateTypes(ddl)
	ddl = ddlSmallInt.ReplaceAllString(ddl, "INTEGER")
	ddl = ddlUUID.ReplaceAllString(ddl, "TEXT")
	// SQLite 的 CREATE INDEX 不支持 NULLS LAST/FIRST（SELECT 中支持，无需处理）
	ddl = ddlNullsOrder.ReplaceAllString(ddl, "")
	ddl = ddlDefaultNow.ReplaceAllString(ddl, "DEFAULT CURRENT_TIMESTAMP")
	// UUID 主键/默认值由 Go 层保证 (uuid.New())，SQLite DEFAULT 不允许随机函数
	ddl = ddlDefaultG.ReplaceAllString(ddl, "")
	return ddl
}
