CREATE TABLE picking_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    picking_no VARCHAR(20) NOT NULL UNIQUE,
    type VARCHAR(20) NOT NULL DEFAULT 'by_product',
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    order_date DATE NOT NULL DEFAULT CURRENT_DATE,
    assigned_to VARCHAR(100),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE TABLE picking_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    picking_id UUID NOT NULL REFERENCES picking_orders(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    product_name VARCHAR(200) NOT NULL,
    product_code VARCHAR(20) NOT NULL,
    required_quantity DECIMAL(15,3) NOT NULL,
    picked_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    source_order_id UUID,
    source_customer_id UUID,
    source_customer_name VARCHAR(200),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_picking_orders_date ON picking_orders(order_date);
CREATE INDEX idx_picking_items_picking ON picking_items(picking_id);
CREATE INDEX idx_picking_items_product ON picking_items(product_id);
