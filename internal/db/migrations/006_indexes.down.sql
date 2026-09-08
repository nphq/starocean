DROP INDEX IF EXISTS idx_inventory_movements_created;
DROP INDEX IF EXISTS idx_purchase_order_items_order;
DROP INDEX IF EXISTS idx_sales_order_items_order;

CREATE INDEX IF NOT EXISTS idx_suppliers_code ON suppliers(code);
CREATE INDEX IF NOT EXISTS idx_customers_code ON customers(code);
CREATE INDEX IF NOT EXISTS idx_attendance_source ON attendance_records(source);
CREATE INDEX IF NOT EXISTS idx_payments_type ON payments(type);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_status ON purchase_orders(status);
CREATE INDEX IF NOT EXISTS idx_sales_orders_status ON sales_orders(status);
