CREATE TABLE workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(50) NOT NULL,
    from_status VARCHAR(20) NOT NULL,
    to_status VARCHAR(20) NOT NULL,
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(entity_type, from_status, to_status, company_id)
);

INSERT INTO workflows (entity_type, from_status, to_status) VALUES
('sales_order', 'draft', 'confirmed'),
('sales_order', 'draft', 'cancelled'),
('sales_order', 'confirmed', 'shipped'),
('sales_order', 'confirmed', 'cancelled'),
('sales_order', 'shipped', 'invoiced'),
('sales_order', 'shipped', 'cancelled'),
('sales_order', 'cancelled', 'draft');

INSERT INTO workflows (entity_type, from_status, to_status) VALUES
('purchase_order', 'draft', 'confirmed'),
('purchase_order', 'draft', 'cancelled'),
('purchase_order', 'confirmed', 'received'),
('purchase_order', 'confirmed', 'cancelled'),
('purchase_order', 'received', 'paid'),
('purchase_order', 'cancelled', 'draft');
