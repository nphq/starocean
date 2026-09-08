-- 财务聚合：按类型+日期范围查询
CREATE INDEX IF NOT EXISTS idx_payments_type_date ON payments(type, payment_date DESC);

-- 库存流水：按产品查流水（覆盖 inventory by product 页面）
CREATE INDEX IF NOT EXISTS idx_inventory_movements_product_created ON inventory_movements(product_id, created_at DESC);

-- 考勤：排序字段
CREATE INDEX IF NOT EXISTS idx_attendance_checkin_time ON attendance_records(check_in_time DESC);

-- 全局搜索：客户/供应商 name trigram 索引
CREATE INDEX IF NOT EXISTS idx_customers_name_gist ON customers USING gist (name gist_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_suppliers_name_gist ON suppliers USING gist (name gist_trgm_ops);

-- 全局搜索：订单号 trigram 索引
CREATE INDEX IF NOT EXISTS idx_sales_orders_order_no_gist ON sales_orders USING gist (order_no gist_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_order_no_gist ON purchase_orders USING gist (order_no gist_trgm_ops);

-- 低库存：generated column + 索引
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_low_stock BOOLEAN GENERATED ALWAYS AS (COALESCE(current_stock, 0) <= safety_stock) STORED;
CREATE INDEX IF NOT EXISTS idx_products_low_stock ON products(is_low_stock) WHERE is_low_stock = true;
