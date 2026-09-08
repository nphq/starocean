-- 028 回滚（PostgreSQL golang-migrate 使用；SQLite 迁移只读 .up.sql）。
DROP INDEX IF EXISTS idx_payments_client_token;
ALTER TABLE payments DROP COLUMN client_token;
