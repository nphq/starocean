-- 003_performance.up.sql
CREATE INDEX idx_products_created_at ON products (created_at DESC NULLS LAST);
DROP INDEX IF EXISTS idx_products_code;
