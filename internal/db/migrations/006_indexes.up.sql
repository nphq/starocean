-- 删除冗余/低选择性索引
DROP INDEX IF EXISTS idx_sales_orders_status;
DROP INDEX IF EXISTS idx_purchase_orders_status;
DROP INDEX IF EXISTS idx_payments_type;
DROP INDEX IF EXISTS idx_attendance_source;
DROP INDEX IF EXISTS idx_customers_code;
DROP INDEX IF EXISTS idx_suppliers_code;

-- 补充缺失的外键索引
CREATE INDEX idx_sales_order_items_order ON sales_order_items(order_id);
CREATE INDEX idx_purchase_order_items_order ON purchase_order_items(order_id);

-- 补充库存流水排序索引
CREATE INDEX idx_inventory_movements_created ON inventory_movements(created_at DESC);
