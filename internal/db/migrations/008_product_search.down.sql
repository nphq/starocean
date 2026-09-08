DROP INDEX IF EXISTS idx_products_search_gin;
ALTER TABLE products DROP COLUMN IF EXISTS search_text;
CREATE INDEX idx_products_name_gist ON products USING gist (name gist_trgm_ops);
CREATE INDEX idx_products_code_gist ON products USING gist (code gist_trgm_ops);
