package shared

// ---------------------------------------------------------------------------
// 数据库方言标记: 由 internal/db.Connect 根据 DSN 设置。
// 业务层需要针对 SQLite/PostgreSQL 写不同查询时使用 (如 generate_series)。
// ---------------------------------------------------------------------------

var sqliteMode bool

// SetSQLiteMode 由 internal/db 在连接时调用。
func SetSQLiteMode(v bool) { sqliteMode = v }

// IsSQLite 返回当前数据库是否为 SQLite 单文件模式。
func IsSQLite() bool { return sqliteMode }
