DROP INDEX IF EXISTS idx_products_low_stock;
ALTER TABLE products DROP COLUMN IF EXISTS is_low_stock;
DROP INDEX IF EXISTS idx_purchase_orders_order_no_gist;
DROP INDEX IF EXISTS idx_sales_orders_order_no_gist;
DROP INDEX IF EXISTS idx_suppliers_name_gist;
DROP INDEX IF EXISTS idx_customers_name_gist;
DROP INDEX IF EXISTS idx_attendance_checkin_time;
DROP INDEX IF EXISTS idx_inventory_movements_product_created;
DROP INDEX IF EXISTS idx_payments_type_date;
