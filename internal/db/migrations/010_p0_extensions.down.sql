ALTER TABLE customers DROP COLUMN IF EXISTS company_id;
ALTER TABLE suppliers DROP COLUMN IF EXISTS company_id;
ALTER TABLE products DROP COLUMN IF EXISTS company_id;
ALTER TABLE sales_orders DROP COLUMN IF EXISTS company_id;
ALTER TABLE purchase_orders DROP COLUMN IF EXISTS company_id;
ALTER TABLE inventory_movements DROP COLUMN IF EXISTS company_id;
ALTER TABLE payments DROP COLUMN IF EXISTS company_id;
ALTER TABLE attendance_records DROP COLUMN IF EXISTS company_id;
ALTER TABLE order_sequences DROP COLUMN IF EXISTS company_id;

ALTER TABLE products DROP COLUMN IF EXISTS properties;
ALTER TABLE customers DROP COLUMN IF EXISTS properties;
ALTER TABLE suppliers DROP COLUMN IF EXISTS properties;
ALTER TABLE sales_orders DROP COLUMN IF EXISTS properties;
ALTER TABLE purchase_orders DROP COLUMN IF EXISTS properties;
