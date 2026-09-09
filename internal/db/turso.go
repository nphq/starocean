package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	turso "turso.tech/database/tursogo"
)

// ---------------------------------------------------------------------------
// Turso (SQLite 兼容引擎) 支持
//
// 设计:
//   1. 业务 SQL 直接写 Turso/SQLite 方言（strftime / date('now',…) / LIKE /
//      json_patch / CAST(… AS …) / julianday 等），不再接受 PostgreSQL 方言。
//   2. 驱动包装层仅做两件事: $N→? 占位符重排（见 renumberPlaceholders），
//      以及把日期形状的文本结果扫成 time.Time（见 tursoRewriteRows）。
//   3. 迁移文件仍为历史 PG 风格 DDL，由 turso_migrate.translateDDL 翻译后执行
//      （GENERATED STORED → 普通列+触发器，GL 种子 → 静态 INSERT 等）。
//
// Turso 为 SQLite 兼容的纯 Go 驱动 (purego, 无 CGO)，单文件本地运行；
// 写事务经 BeginTx 下发 BEGIN IMMEDIATE，串行化读-改-写（库存/收付）。
// ---------------------------------------------------------------------------

func isTursoDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "sqlite:") || strings.HasPrefix(dsn, "turso:")
}

// Connect 按 DSN 连接数据库，支持 sqlite:<path>（兼容前缀，Turso 引擎承载）
// 与 turso:<path>（可携带 ?experimental= 等 Turso DSN 参数）。
func Connect(databaseURL string) (*sql.DB, error) {
	if !isTursoDSN(databaseURL) {
		return nil, fmt.Errorf("不支持的 DSN %q：仅支持 sqlite:<path> 或 turso:<path>", databaseURL)
	}
	database, err := OpenTurso(databaseURL)
	if err != nil {
		return nil, err
	}
	log.Println("turso connected:", tursoPath(databaseURL))
	return database, nil
}

// tursoPath 把 sqlite:<path> / turso:<path> 转换为 tursogo DSN（本地文件路径，
// 原样透传 ?query 参数；空路径默认 starocean.db）。
func tursoPath(dsn string) string {
	p := dsn
	if strings.HasPrefix(p, "sqlite:") {
		p = strings.TrimPrefix(p, "sqlite:")
	} else {
		p = strings.TrimPrefix(p, "turso:")
	}
	if p == "" {
		p = "starocean.db"
	}
	if strings.HasPrefix(p, "//") {
		p = p[1:]
	}
	return p
}

// OpenTurso 打开 Turso 单文件数据库。
func OpenTurso(dsn string) (*sql.DB, error) {
	connector, err := turso.NewConnector(tursoPath(dsn), turso.WithBusyTimeout(5000))
	if err != nil {
		return nil, fmt.Errorf("open turso: %w", err)
	}
	database := sql.OpenDB(&tursoRewriteConnector{inner: connector})
	// 只读查询多连接并行; 写事务 BEGIN IMMEDIATE 串行化 (见 tursoRewriteConn.BeginTx)
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	if err := database.Ping(); err != nil {
		if strings.Contains(err.Error(), "Stored generated columns") {
			return nil, fmt.Errorf("ping turso: %w（旧版库文件含 STORED 生成列，Turso 无法打开；开发库直接删除 %s* 后重建，生产库按 README 升级注意迁移数据）",
				err, tursoPath(dsn))
		}
		return nil, fmt.Errorf("ping turso: %w", err)
	}
	return database, nil
}

type tursoRewriteConnector struct{ inner driver.Connector }

func (c *tursoRewriteConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := &tursoRewriteConn{Conn: conn}
	// 外键约束默认关闭与此前 SQLite(_fk=1) 语义对齐：每条池连接尽力开启，
	// 失败则忽略（不阻断建连，业务正确性不依赖外键）。
	_ = wrapped.exec("PRAGMA foreign_keys=ON")
	return wrapped, nil
}

func (c *tursoRewriteConnector) Driver() driver.Driver { return c.inner.Driver() }

func (c *tursoRewriteConn) Prepare(query string) (driver.Stmt, error) {
	rewritten, perm := renumberPlaceholders(query)
	inner, err := c.Conn.Prepare(rewritten)
	if err != nil {
		return nil, err
	}
	return &tursoRewriteStmt{Stmt: inner, perm: perm}, nil
}

// Begin 与 database/sql 的默认 (deferred) 行为不同: 延迟事务
// 在"读后写"时遇到并发写者会产生忙冲突(无法重试)，
// 而 BEGIN IMMEDIATE 在事务开始就排队等待写锁，拿到锁后从头读取，
// 既串行化写事务(无丢失更新)，又能让无事务的只读查询运行在其他连接上。
type tursoRewriteConn struct {
	driver.Conn
}

func (c *tursoRewriteConn) exec(raw string) error {
	inner, err := c.Conn.Prepare(raw)
	if err != nil {
		return err
	}
	defer func() { _ = inner.Close() }()
	// 复用包装层 ExecContext（优先 Context 变体，规避 deprecated 接口）
	_, err = (&tursoRewriteStmt{Stmt: inner}).ExecContext(context.Background(), nil)
	return err
}

func (c *tursoRewriteConn) Begin() (driver.Tx, error) {
	if err := c.exec("BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	return &tursoRewriteTx{conn: c}, nil
}

func (c *tursoRewriteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type tursoRewriteTx struct{ conn *tursoRewriteConn }

func (t *tursoRewriteTx) Commit() error   { return t.conn.exec("COMMIT") }
func (t *tursoRewriteTx) Rollback() error { return t.conn.exec("ROLLBACK") }

// tursoRewriteStmt 在查询结果层把"日期形状"的文本统一转换为 time.Time。
// 原因: Turso/SQLite 只按列声明的类型转换，COALESCE(created_at, '1970-01-01') 等
// 表达式列返回字符串，而业务代码统一 Scan 到 time.Time。
//
// perm 为占位符重排映射（见 renumberPlaceholders）：新位置 i 取原第 perm[i] 个参数。
type tursoRewriteStmt struct {
	driver.Stmt
	perm []int
}

// NumInput 覆盖内嵌驱动的计数：调用方按原 $N 最大编号传参，
// 而内部语句经重排后槽位更多（重复编号展开）。database/sql 据此预校验参数个数。
func (s *tursoRewriteStmt) NumInput() int {
	if s.perm != nil {
		max := 0
		for _, n := range s.perm {
			if n > max {
				max = n
			}
		}
		return max
	}
	return s.Stmt.NumInput()
}

// reorderArgs 按 perm 把调用方参数重排为引擎出现顺序，并重编 Ordinal。
// 输出长度按占位槽位数（同一编号可出现多次，如 SET a=$4 ... - $4）。
func (s *tursoRewriteStmt) reorderArgs(args []driver.NamedValue) []driver.NamedValue {
	if s.perm == nil {
		return args
	}
	out := make([]driver.NamedValue, len(s.perm))
	for i, n := range s.perm {
		var v driver.Value
		if n >= 1 && n <= len(args) {
			v = args[n-1].Value
		}
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

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
func (s *tursoRewriteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	args = s.reorderArgs(args)
	if ec, ok := s.Stmt.(driver.StmtExecContext); ok {
		return ec.ExecContext(ctx, args)
	}
	//nolint:staticcheck // 仅当驱动不实现 StmtExecContext 时回退
	return s.Stmt.Exec(namedToValues(args))
}

func (s *tursoRewriteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	args = s.reorderArgs(args)
	var (
		rows driver.Rows
		err  error
	)
	if qc, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err = qc.QueryContext(ctx, args)
	} else {
		//nolint:staticcheck // 仅当驱动不实现 StmtQueryContext 时回退
		rows, err = s.Stmt.Query(namedToValues(args))
	}
	if err != nil {
		return nil, err
	}
	return &tursoRewriteRows{Rows: rows}, nil
}

func (s *tursoRewriteStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valueToNamed(args))
}

type tursoRewriteRows struct{ driver.Rows }

func (r *tursoRewriteRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	for i, v := range dest {
		if s, ok := v.(string); ok {
			if t, ok := parseTursoTime(s); ok {
				dest[i] = t
			}
		}
	}
	return nil
}

// reDateLike 只匹配完整日期/日期时间形状（年-月-日 开头），
// 不会误伤 'YYYY-MM' 这类月份标签。
var reDateLike = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([ T]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$`)

var tursoTimeLayouts = []string{
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

// parseTursoTime 解析 Turso/SQLite 日期/时间文本；非日期文本原样返回 (ok=false)。
func parseTursoTime(s string) (time.Time, bool) {
	if !reDateLike.MatchString(s) {
		return time.Time{}, false
	}
	for _, layout := range tursoTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ---------------------------------------------------------------------------
// $N 占位符 → ?（Turso 按出现顺序绑参）
// ---------------------------------------------------------------------------

// renumberPlaceholders 将 $N 占位符按出现顺序改写为 ?，并返回
// 新位置→原编号映射（如 SET x=$2 ... WHERE id=$1，若不重排会参数错位）。
// 字符串字面量内的 $N 不处理；无 $N 时返回原串与 nil（? 查询走恒等快路径）。
func renumberPlaceholders(q string) (string, []int) {
	fast := true
	for i := 0; i < len(q); i++ {
		if q[i] == '$' && i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9' {
			fast = false
			break
		}
	}
	if fast {
		return q, nil
	}
	var (
		b     strings.Builder
		perm  []int
		inStr bool
		i     int
	)
	b.Grow(len(q))
	for i < len(q) {
		c := q[i]
		if inStr {
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(q) && q[i+1] == '\'' {
					b.WriteByte(q[i+1])
					i++
				} else {
					inStr = false
				}
			}
			i++
			continue
		}
		if c == '\'' {
			inStr = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '$' && i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9' {
			j := i + 1
			n := 0
			for j < len(q) && q[j] >= '0' && q[j] <= '9' {
				n = n*10 + int(q[j]-'0')
				j++
			}
			b.WriteByte('?')
			perm = append(perm, n)
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	if perm == nil {
		return q, nil
	}
	return b.String(), perm
}
