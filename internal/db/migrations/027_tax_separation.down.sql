-- 027_tax_separation 回滚（PostgreSQL golang-migrate 使用；SQLite 迁移只读 .up.sql）。
ALTER TABLE gl_settings DROP COLUMN input_tax_account;
ALTER TABLE gl_settings DROP COLUMN output_tax_account;
ALTER TABLE products DROP COLUMN default_tax_rate;
ALTER TABLE purchase_order_items DROP COLUMN tax_rate;
ALTER TABLE sales_order_items DROP COLUMN tax_rate;
