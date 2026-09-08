-- 合并搜索列，消除 OR 条件
ALTER TABLE products ADD COLUMN IF NOT EXISTS search_text text GENERATED ALWAYS AS (name || ' ' || COALESCE(code, '')) STORED;

-- GiST 换 GIN（读性能更优）
DROP INDEX IF EXISTS idx_products_name_gist;
DROP INDEX IF EXISTS idx_products_code_gist;
CREATE INDEX idx_products_search_gin ON products USING gin (search_text gin_trgm_ops);
