-- 026_clearing: AR/AP 核销明细 + payments 往来方外键
--
-- 1) payments 补齐 partner_type/partner_id（多态软关联，与 invoices 同款做法）。
-- 2) 新建 finance_clearings：每笔挂单收付款冲抵单据的权威明细；
--    paid_amount 保留为查询冗余列，clearing 表为审计明细。
-- 3) 幂等：UNIQUE(payment_id, doc_type, doc_id) 防止同一付款对同一单据重复核销。
-- 4) 存量回填：带 reference 的付款生成 clearing + 按单据反查 partner；
--    无 reference 的付款按 partner_name 精确匹配客户/供应商，仅唯一命中才回填。

ALTER TABLE payments ADD COLUMN partner_type VARCHAR(10) NOT NULL DEFAULT '';
ALTER TABLE payments ADD COLUMN partner_id UUID;

CREATE TABLE finance_clearings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL REFERENCES payments(id),
    doc_type VARCHAR(20) NOT NULL CHECK (doc_type IN ('sales_order','purchase_order')),
    doc_id UUID NOT NULL,
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active','reversed')),
    cleared_by VARCHAR(100) NOT NULL DEFAULT '',
    cleared_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);
CREATE INDEX idx_clearings_doc ON finance_clearings(doc_type, doc_id);
CREATE INDEX idx_clearings_payment ON finance_clearings(payment_id);
CREATE UNIQUE INDEX idx_clearings_payment_doc ON finance_clearings(payment_id, doc_type, doc_id);

-- 存量回填：带 reference 的付款 → 生成 clearing（幂等：仅无既有 clearing 的付款）
INSERT INTO finance_clearings (id, payment_id, doc_type, doc_id, amount, status, cleared_by, cleared_at, company_id)
SELECT gen_random_uuid(), p.id, p.reference_type, p.reference_id, p.amount, 'active', '', p.created_at, COALESCE(p.company_id,'default')
FROM payments p
WHERE p.reference_type IN ('sales_order','purchase_order')
  AND p.reference_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM finance_clearings c WHERE c.payment_id = p.id AND c.doc_type = p.reference_type AND c.doc_id = p.reference_id);

-- 存量回填：带 reference 的付款 → 按单据反查 partner
UPDATE payments
SET partner_type = CASE WHEN reference_type = 'sales_order' THEN 'customer' ELSE 'supplier' END,
    partner_id  = CASE
        WHEN reference_type = 'sales_order' THEN (SELECT customer_id FROM sales_orders s WHERE s.id = payments.reference_id)
        ELSE (SELECT supplier_id FROM purchase_orders po WHERE po.id = payments.reference_id)
    END
WHERE reference_type IN ('sales_order','purchase_order')
  AND reference_id IS NOT NULL;

-- 存量回填：无 reference 的付款 → 按 partner_name 精确匹配、仅唯一命中才回填（客户侧）
UPDATE payments
SET partner_type = 'customer',
    partner_id = (SELECT id FROM customers c WHERE c.name = payments.partner_name)
WHERE partner_name <> '' AND partner_id IS NULL
  AND (SELECT COUNT(*) FROM customers c WHERE c.name = payments.partner_name) = 1;

-- 存量回填：无 reference 的付款 → 客户无唯一命中时按供应商唯一命中
UPDATE payments
SET partner_type = 'supplier',
    partner_id = (SELECT id FROM suppliers s WHERE s.name = payments.partner_name)
WHERE partner_name <> '' AND partner_id IS NULL
  AND (SELECT COUNT(*) FROM customers c WHERE c.name = payments.partner_name) <> 1
  AND (SELECT COUNT(*) FROM suppliers s WHERE s.name = payments.partner_name) = 1;
