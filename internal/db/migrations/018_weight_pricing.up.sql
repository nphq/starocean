-- 商品增加计量类型
ALTER TABLE products ADD COLUMN IF NOT EXISTS pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';

-- 销售订单明细增加称重字段
ALTER TABLE sales_order_items ADD COLUMN IF NOT EXISTS actual_quantity DECIMAL(15,3);
ALTER TABLE sales_order_items ADD COLUMN IF NOT EXISTS actual_amount DECIMAL(15,2);
ALTER TABLE sales_order_items ADD COLUMN IF NOT EXISTS pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';

-- 采购订单明细增加称重字段
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS actual_quantity DECIMAL(15,3);
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS actual_amount DECIMAL(15,2);
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';
