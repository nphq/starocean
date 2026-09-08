-- 阶梯价格表
CREATE TABLE product_price_tiers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    min_quantity INT NOT NULL DEFAULT 1,
    max_quantity INT,
    unit_price DECIMAL(15,2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_price_tiers_product ON product_price_tiers(product_id);

-- 客户专属价格表
CREATE TABLE customer_product_prices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    unit_price DECIMAL(15,2) NOT NULL,
    effective_from DATE,
    effective_to DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    UNIQUE(customer_id, product_id)
);

CREATE INDEX idx_customer_price_lookup ON customer_product_prices(customer_id, product_id);
