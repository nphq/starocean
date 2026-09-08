-- 003_performance.down.sql
DROP INDEX IF EXISTS idx_products_created_at;
CREATE INDEX IF NOT EXISTS idx_products_code ON products (code);
