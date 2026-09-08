package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"time"

	moderncsqlite "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// SQLite (现代 single-machine 模式) 支持
//
// 设计: 业务 SQL 全部按 PostgreSQL 编写，本包通过两层机制让同一份代码
// 在 SQLite 上运行:
//   1. 驱动包装层: 在 driver.Conn.Prepare 处对 SQL 文本做方言翻译
//      (::casts / ILIKE / NOW() / FOR UPDATE / gen_random_uuid / INTERVAL /
//       jsonb || 等 → SQLite 等价形式)。
//   2. DDL 翻译层: 迁移文件中的 PostgreSQL DDL 翻译为 SQLite DDL 后执行
//      (见 sqlite_migrate.go)。
//
// SQLite 单写者模型: 只读查询走连接池并行执行(页面加载不再被单连接串行放大)，
// 写事务通过 BeginTx 的 BEGIN IMMEDIATE 在事务开始时即持写锁串行化,
// 业务层读-改-写(如库存扣减、paid_amount 累加)无需行锁即保证一致性
// (这也正是 FOR UPDATE 在 SQLite 语法中直接被移除的原因)。
// ---------------------------------------------------------------------------

func isSQLiteDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "sqlite:")
}

// sqliteURI 把 sqlite:<path> 转换为 modernc.org/sqlite 的 file: URI，
// 并强制开启外键、WAL、busy_timeout(5s)。
func sqliteURI(dsn string) string {
	p := strings.TrimPrefix(dsn, "sqlite:")
	if p == "" {
		p = "starocean.db"
	}
	if strings.HasPrefix(p, "//") {
		p = p[1:]
	}
	sep := "?"
	if strings.Contains(p, "?") {
		sep = "&"
	}
	// _time_format=sqlite: time.Time 参数统一写为 'YYYY-MM-DD HH:MM:SS.SSS±HH:MM'
	return "file:" + p + sep + "_fk=1&_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_time_format=sqlite"
}

// OpenSQLite 打开单文件 SQLite 数据库。
func OpenSQLite(dsn string) (*sql.DB, error) {
	connector, err := moderncsqlite.NewConnector(sqliteURI(dsn))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database := sql.OpenDB(&sqliteRewriteConnector{inner: connector})
	// 只读查询多连接并行; 写事务 BEGIN IMMEDIATE 串行化 (见 sqliteRewriteConn.BeginTx)
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return database, nil
}

type sqliteRewriteConnector struct{ inner driver.Connector }

func (c *sqliteRewriteConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &sqliteRewriteConn{Conn: conn}, nil
}

func (c *sqliteRewriteConnector) Driver() driver.Driver { return c.inner.Driver() }

func (c *sqliteRewriteConn) Prepare(query string) (driver.Stmt, error) {
	inner, err := c.Conn.Prepare(rewriteQuery(query))
	if err != nil {
		return nil, err
	}
	return &sqliteRewriteStmt{Stmt: inner}, nil
}

// Begin 与 database/sql 的默认 (deferred) 行为不同: SQLite 中延迟事务
// 在"读后写"时遇到并发写者会产生 SQLITE_BUSY_SNAPSHOT(无法重试)，
// 而 BEGIN IMMEDIATE 在事务开始就排队等待写锁，拿到锁后从头读取，
// 既串行化写事务(无丢失更新)，又能让无事务的只读查询运行在其他连接上。
type sqliteRewriteConn struct {
	driver.Conn
}

func (c *sqliteRewriteConn) exec(raw string) error {
	inner, err := c.Conn.Prepare(raw)
	if err != nil {
		return err
	}
	defer func() { _ = inner.Close() }()
	// 复用包装层 ExecContext（优先 Context 变体，规避 deprecated 接口）
	_, err = (&sqliteRewriteStmt{Stmt: inner}).ExecContext(context.Background(), nil)
	return err
}

func (c *sqliteRewriteConn) Begin() (driver.Tx, error) {
	if err := c.exec("BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	return &sqliteRewriteTx{conn: c}, nil
}

func (c *sqliteRewriteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type sqliteRewriteTx struct{ conn *sqliteRewriteConn }

func (t *sqliteRewriteTx) Commit() error   { return t.conn.exec("COMMIT") }
func (t *sqliteRewriteTx) Rollback() error { return t.conn.exec("ROLLBACK") }

// sqliteRewriteStmt 在查询结果层把"日期形状"的文本统一转换为 time.Time。
// 原因: SQLite 只按列声明的类型转换，COALESCE(created_at, '1970-01-01') 等
// 表达式列返回字符串，而业务代码统一 Scan 到 time.Time。
type sqliteRewriteStmt struct{ driver.Stmt }

func namedToValues(args []driver.NamedValue) []driver.Value {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	return vals
}

func valueToNamed(args []driver.Value) []driver.NamedValue {
	vals := make([]driver.NamedValue, len(args))
	for i, v := range args {
		vals[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return vals
}

// ExecContext/QueryContext 优先走底层 Context 变体，避免 deprecated 接口。
func (s *sqliteRewriteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := s.Stmt.(driver.StmtExecContext); ok {
		return ec.ExecContext(ctx, args)
	}
	//nolint:staticcheck // 仅当驱动不实现 StmtExecContext 时回退（modernc 会走上面分支）
	return s.Stmt.Exec(namedToValues(args))
}

func (s *sqliteRewriteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	var (
		rows driver.Rows
		err  error
	)
	if qc, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err = qc.QueryContext(ctx, args)
	} else {
		//nolint:staticcheck // 仅当驱动不实现 StmtQueryContext 时回退（modernc 会走上面分支）
		rows, err = s.Stmt.Query(namedToValues(args))
	}
	if err != nil {
		return nil, err
	}
	return &sqliteRewriteRows{Rows: rows}, nil
}

func (s *sqliteRewriteStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valueToNamed(args))
}

type sqliteRewriteRows struct{ driver.Rows }

func (r *sqliteRewriteRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	for i, v := range dest {
		if s, ok := v.(string); ok {
			if t, ok := parseSQLiteTime(s); ok {
				dest[i] = t
			}
		}
	}
	return nil
}

// reDateLike 只匹配完整日期/日期时间形状（年-月-日 开头），
// 不会误伤 'YYYY-MM' 这类月份标签。
var reDateLike = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([ T]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$`)

var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02",
}

// parseSQLiteTime 解析 SQLite 日期/时间文本；非日期文本原样返回 (ok=false)。
func parseSQLiteTime(s string) (time.Time, bool) {
	if !reDateLike.MatchString(s) {
		return time.Time{}, false
	}
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ---------------------------------------------------------------------------
// PostgreSQL → SQLite 查询文本翻译
// ---------------------------------------------------------------------------

// reCast 匹配 SQL 中的 ::type 强制转换。左操作数支持:
//
//	标识符(可带函数调用如 COALESCE(...)) / $参数 / 字符串字面量。
//
// 注意: 不能单独匹配裸括号组，否则会把 COALESCE(a,b)::text 的内层 (a,b) 误当作左操作数。
var reCast = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*(?:\([^()]*\))?|'[^']*'|\$[0-9]+)::([a-zA-Z_][a-zA-Z0-9_]*)`)

// castTypes: PostgreSQL 类型 → SQLite CAST 目标类型。
// 时间/日期/JSON/UUID 一律走 TEXT (SQLite 无原生类型，文本可比可扫)。
var castTypes = map[string]string{
	"text": "TEXT", "varchar": "TEXT", "char": "TEXT", "character": "TEXT",
	"jsonb": "TEXT", "json": "TEXT", "uuid": "TEXT", "citext": "TEXT",
	"timestamptz": "TEXT", "timestamp": "TEXT", "date": "TEXT",
	"bool": "TEXT", "boolean": "TEXT",
	"numeric": "NUMERIC", "decimal": "NUMERIC", "money": "NUMERIC",
	"int": "INTEGER", "integer": "INTEGER", "bigint": "INTEGER", "smallint": "INTEGER",
	"real": "REAL", "float": "REAL", "double": "REAL",
}

// reJsonbConcat 匹配 properties || $n::jsonb (JSONB 合并)
var reJsonbConcat = regexp.MustCompile(`((?:\w+\.)?properties)\s*\|\|\s*(\$\d+)\s*::jsonb`)

// reDateTrunc 匹配 DATE_TRUNC('month'|'week', CURRENT_DATE)[::date]
var reDateTruncMonth = regexp.MustCompile(`(?i)DATE_TRUNC\('month',\s*CURRENT_DATE\)(::date)?`)
var reDateTruncWeek = regexp.MustCompile(`(?i)DATE_TRUNC\('week',\s*CURRENT_DATE\)(::date)?`)

// reMonthInterval 匹配 date('now','start of month') ± INTERVAL 'N months'
var reMonthInterval = regexp.MustCompile(`(?i)date\('now','start of month'\)\s*([+-])\s*INTERVAL\s*'([0-9]+)\s*months?'`)

// reInterval 匹配 标识符/列名 ± INTERVAL 'N unit'   (仅剩 d.month / CURRENT_DATE 等简单左操作数)
var reInterval = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_.]*)\s*([+-])\s*INTERVAL\s*'([0-9]+)\s*(months?|days?)'`)

// reToChar 匹配 TO_CHAR(x, 'YYYY-MM')
var reToChar = regexp.MustCompile(`(?i)TO_CHAR\(([^,]+),\s*'YYYY-MM'\s*\)`)

// reForUpdate 匹配行尾的 FOR UPDATE (SQLite 单写者模型下无需行锁，且不支持该语法)
var reForUpdate = regexp.MustCompile(`(?is)\s+FOR\s+UPDATE\s*$`)

var reGenUUID = regexp.MustCompile(`gen_random_uuid\(\)`)

var reIlike = regexp.MustCompile(`(?i)ILIKE`)

// reNow 匹配 NOW() (SQLite 用 strftime 生成 RFC3339 文本)
var reNow = regexp.MustCompile(`(?i)NOW\(\)`)

// sqliteUUIDExpr 生成 UUID 形文本 (8-4-4-4-12)，google/uuid.UUID.Scan 可解析。
const sqliteUUIDExpr = `(lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))`

// reTimeCast 匹配 ::time (取时间部分，SQLite 用 time() 函数)
var reTimeCast = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)::time\b`)

// rewriteQuery 将 PostgreSQL 方言 SQL 翻译为 SQLite 可执行 SQL。
// 规则按依赖顺序执行 (先合并 jsonb、先展开 DATE_TRUNC/INTERVAL，再做 ::cast)。
func rewriteQuery(q string) string {
	// 1. JSONB 合并: properties || $n::jsonb → json_patch(properties, $n)
	q = reJsonbConcat.ReplaceAllString(q, `json_patch($1, $2)`)

	// 2. DATE_TRUNC('week','month', CURRENT_DATE)[::date] → date('now',...)
	//    周一作为一周开始: date('now','-' || ((strftime('%w','now')+6)%7) || ' days')
	q = reDateTruncWeek.ReplaceAllString(q, `(date('now','-' || ((strftime('%w','now')+6)%7) || ' days'))`)
	q = reDateTruncMonth.ReplaceAllString(q, `date('now','start of month')`)

	// 3. date('now','start of month') ± INTERVAL 'N months' → 直接合成修饰符
	q = reMonthInterval.ReplaceAllString(q, `date('now','start of month','$1$2 months')`)

	// 4. 简单左操作数 ± INTERVAL 'N unit' → date(x, '±N unit')
	q = reInterval.ReplaceAllString(q, `date($1, '$2$3 $4')`)

	// 5. TO_CHAR(x,'YYYY-MM') → strftime('%Y-%m', x)
	q = reToChar.ReplaceAllString(q, `strftime('%Y-%m', $1)`)

	// 5b. x::time → time(x)  (时间部分比较)
	q = reTimeCast.ReplaceAllString(q, `time($1)`)

	// 6. 移除 FOR UPDATE
	q = reForUpdate.ReplaceAllString(q, ``)

	// 7. gen_random_uuid() → SQLite 表达式
	q = reGenUUID.ReplaceAllString(q, sqliteUUIDExpr)

	// 8. ILIKE → LIKE (SQLite 默认对 ASCII 大小写不敏感，等价于 PG ILIKE)
	q = reIlike.ReplaceAllString(q, `LIKE`)

	// 9. NOW() → RFC3339 文本
	q = reNow.ReplaceAllString(q, `(strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)

	// 10. ::type → CAST(left AS sqlite_type)（时间/日期/JSON/UUID 映射为 TEXT，
	//     否则 SQLite 的 NUMERIC affinity 会把日期字符串转成 0）
	q = reCast.ReplaceAllStringFunc(q, func(m string) string {
		parts := reCast.FindStringSubmatch(m)
		target, ok := castTypes[strings.ToLower(parts[2])]
		if !ok {
			target = "TEXT"
		}
		return "CAST(" + parts[1] + " AS " + target + ")"
	})

	return q
}
