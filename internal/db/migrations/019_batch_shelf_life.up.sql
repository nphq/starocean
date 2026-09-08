-- 商品增加保质期天数
ALTER TABLE products ADD COLUMN IF NOT EXISTS shelf_life_days INT;

-- 库存批次表
CREATE TABLE inventory_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id),
    warehouse_id VARCHAR(50) NOT NULL DEFAULT 'default',
    batch_no VARCHAR(50) NOT NULL,
    production_date DATE,
    expiry_date DATE,
    quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    original_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    unit_cost DECIMAL(15,2),
    status VARCHAR(20) NOT NULL DEFAULT 'normal',
    reference_type VARCHAR(50),
    reference_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_batches_product ON inventory_batches(product_id, warehouse_id);
CREATE INDEX idx_batches_expiry ON inventory_batches(expiry_date) WHERE status = 'normal';

-- 库存变动记录增加批次关联
ALTER TABLE inventory_movements ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES inventory_batches(id);
