-- 覆盖索引：延迟 JOIN 分页只需扫索引取 id，不回表
CREATE INDEX IF NOT EXISTS idx_products_created_id ON products(created_at DESC NULLS LAST, id DESC);
