-- 027_tax_separation: 价税分离（销项/进项税额自动拆分）
--
-- 口径：订单行 unit_price/amount 维持"价税合计（含税）"语义，total_amount 不变；
-- 价税分离仅发生在记账瞬间（autopost），业务侧零感知。
-- 差额法拆分（逐行）：net = round(amount/(1+rate/100), 2)，tax = amount - net，恒有 net+tax=amount，
-- 保证借贷平衡。tax_rate 缺省 0 → 分录退化为现状两行式，存量账套零变化。

ALTER TABLE sales_order_items ADD COLUMN tax_rate DECIMAL(5,2) NOT NULL DEFAULT 0;
ALTER TABLE purchase_order_items ADD COLUMN tax_rate DECIMAL(5,2) NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN default_tax_rate DECIMAL(5,2) NOT NULL DEFAULT 0;
ALTER TABLE gl_settings ADD COLUMN output_tax_account VARCHAR(20) NOT NULL DEFAULT '22210105';
ALTER TABLE gl_settings ADD COLUMN input_tax_account VARCHAR(20) NOT NULL DEFAULT '22210101';
