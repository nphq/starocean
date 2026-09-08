CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_products_name_gist ON products USING gist (name gist_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_products_code_gist ON products USING gist (code gist_trgm_ops);
