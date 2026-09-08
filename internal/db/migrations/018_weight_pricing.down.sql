ALTER TABLE purchase_order_items DROP COLUMN IF EXISTS pricing_type;
ALTER TABLE purchase_order_items DROP COLUMN IF EXISTS actual_amount;
ALTER TABLE purchase_order_items DROP COLUMN IF EXISTS actual_quantity;

ALTER TABLE sales_order_items DROP COLUMN IF EXISTS pricing_type;
ALTER TABLE sales_order_items DROP COLUMN IF EXISTS actual_amount;
ALTER TABLE sales_order_items DROP COLUMN IF EXISTS actual_quantity;

ALTER TABLE products DROP COLUMN IF EXISTS pricing_type;
