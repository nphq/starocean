-- Add company_id to all business tables for multi-tenancy
ALTER TABLE customers ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE products ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE sales_orders ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE purchase_orders ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE inventory_movements ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE attendance_records ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';
ALTER TABLE order_sequences ADD COLUMN IF NOT EXISTS company_id VARCHAR(50) NOT NULL DEFAULT 'default';

-- Add JSONB properties column for flexible custom fields
ALTER TABLE products ADD COLUMN IF NOT EXISTS properties JSONB NOT NULL DEFAULT '{}';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS properties JSONB NOT NULL DEFAULT '{}';
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS properties JSONB NOT NULL DEFAULT '{}';
ALTER TABLE sales_orders ADD COLUMN IF NOT EXISTS properties JSONB NOT NULL DEFAULT '{}';
ALTER TABLE purchase_orders ADD COLUMN IF NOT EXISTS properties JSONB NOT NULL DEFAULT '{}';
