-- 026_clearing 回滚（PostgreSQL golang-migrate 使用；SQLite 迁移只读 .up.sql）。
DROP TABLE IF EXISTS finance_clearings;
ALTER TABLE payments DROP COLUMN partner_id;
ALTER TABLE payments DROP COLUMN partner_type;
