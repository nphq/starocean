-- StarOcean 基线 schema（Turso 方言，直接可执行）。
--
-- 开发阶段无历史数据兼容负担：全库由本文件一次建出，不做版本化迁移。
-- 幂等：表/触发器/索引一律 IF NOT EXISTS，种子行一律 ON CONFLICT DO NOTHING，
-- Migrate 可重复执行。改表结构直接改本文件（本地库删文件重建）。
--
-- 约定：UUID 主键由 Go 层生成（uuid.New），DDL 中不设随机函数默认值；
-- 金额 NUMERIC、时间/日期/UUID/JSON 一律 TEXT。

-- ============ 主数据 ============

-- Users (single-user auth, 最小 RBAC: admin 改单 / viewer 只看账)
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Customers
CREATE TABLE IF NOT EXISTS customers (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    contact_person TEXT,
    phone TEXT,
    email TEXT,
    address TEXT,
    credit_limit NUMERIC DEFAULT 0,
    balance NUMERIC DEFAULT 0,
    tier TEXT NOT NULL DEFAULT 'normal',
    sales_person TEXT,
    company_id TEXT NOT NULL DEFAULT 'default',
    properties TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Suppliers
CREATE TABLE IF NOT EXISTS suppliers (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    contact_person TEXT,
    phone TEXT,
    email TEXT,
    address TEXT,
    balance NUMERIC DEFAULT 0,
    rating NUMERIC DEFAULT 0,
    on_time_rate NUMERIC DEFAULT 0,
    quality_rate NUMERIC DEFAULT 0,
    company_id TEXT NOT NULL DEFAULT 'default',
    properties TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Products
CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    category TEXT,
    unit TEXT DEFAULT '个',
    sale_price NUMERIC,
    cost_price NUMERIC,
    safety_stock INTEGER DEFAULT 0,
    current_stock INTEGER DEFAULT 0,
    pricing_type TEXT NOT NULL DEFAULT 'standard',
    shelf_life_days INTEGER,
    default_tax_rate NUMERIC NOT NULL DEFAULT 0,
    is_low_stock INTEGER,
    search_text TEXT,
    company_id TEXT NOT NULL DEFAULT 'default',
    properties TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- ============ 单据 ============

-- Sales orders
CREATE TABLE IF NOT EXISTS sales_orders (
    id TEXT PRIMARY KEY,
    order_no TEXT NOT NULL UNIQUE,
    customer_id TEXT REFERENCES customers(id),
    status TEXT DEFAULT 'draft',
    total_amount NUMERIC DEFAULT 0,
    paid_amount NUMERIC DEFAULT 0,
    order_date TEXT DEFAULT CURRENT_DATE,
    delivery_date TEXT,
    notes TEXT,
    company_id TEXT NOT NULL DEFAULT 'default',
    properties TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Sales order items（amount 由触发器维护 = quantity * unit_price）
CREATE TABLE IF NOT EXISTS sales_order_items (
    id TEXT PRIMARY KEY,
    order_id TEXT REFERENCES sales_orders(id) ON DELETE CASCADE,
    product_id TEXT REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price NUMERIC NOT NULL,
    amount NUMERIC,
    tax_rate NUMERIC NOT NULL DEFAULT 0,
    actual_quantity NUMERIC,
    actual_amount NUMERIC,
    pricing_type TEXT NOT NULL DEFAULT 'standard'
);

-- Purchase orders
CREATE TABLE IF NOT EXISTS purchase_orders (
    id TEXT PRIMARY KEY,
    order_no TEXT NOT NULL UNIQUE,
    supplier_id TEXT REFERENCES suppliers(id),
    status TEXT DEFAULT 'draft',
    total_amount NUMERIC DEFAULT 0,
    paid_amount NUMERIC DEFAULT 0,
    order_date TEXT DEFAULT CURRENT_DATE,
    delivery_date TEXT,
    notes TEXT,
    company_id TEXT NOT NULL DEFAULT 'default',
    properties TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Purchase order items（amount 由触发器维护 = quantity * unit_price）
CREATE TABLE IF NOT EXISTS purchase_order_items (
    id TEXT PRIMARY KEY,
    order_id TEXT REFERENCES purchase_orders(id) ON DELETE CASCADE,
    product_id TEXT REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price NUMERIC NOT NULL,
    amount NUMERIC,
    tax_rate NUMERIC NOT NULL DEFAULT 0,
    actual_quantity NUMERIC,
    actual_amount NUMERIC,
    pricing_type TEXT NOT NULL DEFAULT 'standard'
);

-- 订单序号
CREATE TABLE IF NOT EXISTS order_sequences (
    seq_key TEXT PRIMARY KEY,
    last_seq INTEGER NOT NULL DEFAULT 0,
    company_id TEXT NOT NULL DEFAULT 'default'
);

-- ============ 库存 ============

-- Inventory movements
CREATE TABLE IF NOT EXISTS inventory_movements (
    id TEXT PRIMARY KEY,
    product_id TEXT REFERENCES products(id),
    type TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    batch_id TEXT REFERENCES inventory_batches(id),
    before_stock INTEGER NOT NULL,
    after_stock INTEGER NOT NULL,
    company_id TEXT NOT NULL DEFAULT 'default',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Inventory batches（批次/效期）
CREATE TABLE IF NOT EXISTS inventory_batches (
    id TEXT PRIMARY KEY,
    product_id TEXT NOT NULL REFERENCES products(id),
    warehouse_id TEXT NOT NULL DEFAULT 'default',
    batch_no TEXT NOT NULL,
    production_date TEXT,
    expiry_date TEXT,
    quantity NUMERIC NOT NULL DEFAULT 0,
    original_quantity NUMERIC NOT NULL DEFAULT 0,
    unit_cost NUMERIC,
    status TEXT NOT NULL DEFAULT 'normal',
    reference_type TEXT,
    reference_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default'
);

-- ============ 往来 ============

-- Payments
CREATE TABLE IF NOT EXISTS payments (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    amount NUMERIC NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    partner_type TEXT NOT NULL DEFAULT '',
    partner_id TEXT,
    partner_name TEXT,
    notes TEXT,
    payment_date TEXT DEFAULT CURRENT_DATE,
    client_token TEXT,
    company_id TEXT NOT NULL DEFAULT 'default',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- 核销明细：每笔挂单收付款冲抵单据的权威明细（paid_amount 为查询冗余列）
CREATE TABLE IF NOT EXISTS finance_clearings (
    id TEXT PRIMARY KEY,
    payment_id TEXT NOT NULL REFERENCES payments(id),
    doc_type TEXT NOT NULL CHECK (doc_type IN ('sales_order','purchase_order')),
    doc_id TEXT NOT NULL,
    amount NUMERIC NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','reversed')),
    cleared_by TEXT NOT NULL DEFAULT '',
    cleared_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default',
    UNIQUE (payment_id, doc_type, doc_id)
);
CREATE INDEX IF NOT EXISTS idx_clearings_doc ON finance_clearings(doc_type, doc_id);
CREATE INDEX IF NOT EXISTS idx_clearings_payment ON finance_clearings(payment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_clearings_payment_doc ON finance_clearings(payment_id, doc_type, doc_id);

-- 报销
CREATE TABLE IF NOT EXISTS reimbursements (
    id TEXT PRIMARY KEY,
    reimbursement_no TEXT UNIQUE NOT NULL,
    applicant_name TEXT NOT NULL,
    department TEXT NOT NULL DEFAULT '',
    amount NUMERIC NOT NULL DEFAULT 0,
    category TEXT NOT NULL DEFAULT '其他',
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected', 'paid')),
    approver_name TEXT NOT NULL DEFAULT '',
    approved_at TEXT,
    rejected_reason TEXT NOT NULL DEFAULT '',
    payment_id TEXT,
    expense_date TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_reimbursements_status ON reimbursements(status);
CREATE INDEX IF NOT EXISTS idx_reimbursements_applicant ON reimbursements(applicant_name);
CREATE INDEX IF NOT EXISTS idx_reimbursements_date ON reimbursements(expense_date);
CREATE INDEX IF NOT EXISTS idx_reimbursements_company ON reimbursements(company_id);

CREATE TABLE IF NOT EXISTS reimbursement_items (
    id TEXT PRIMARY KEY,
    reimbursement_id TEXT NOT NULL REFERENCES reimbursements(id) ON DELETE CASCADE,
    category TEXT NOT NULL DEFAULT '其他',
    amount NUMERIC NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_reimbursement_items_parent ON reimbursement_items(reimbursement_id);

-- 发票（total_amount 由触发器维护 = amount + tax_amount）
CREATE TABLE IF NOT EXISTS invoices (
    id TEXT PRIMARY KEY,
    invoice_no TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('input', 'output')),
    partner_type TEXT NOT NULL CHECK (partner_type IN ('customer', 'supplier')),
    partner_id TEXT,
    partner_name TEXT NOT NULL DEFAULT '',
    amount NUMERIC NOT NULL DEFAULT 0,
    tax_rate NUMERIC NOT NULL DEFAULT 0,
    tax_amount NUMERIC NOT NULL DEFAULT 0,
    total_amount NUMERIC,
    invoice_date TEXT,
    invoice_code TEXT NOT NULL DEFAULT '',
    invoice_status TEXT NOT NULL DEFAULT 'normal' CHECK (invoice_status IN ('normal', 'void', 'red')),
    reference_type TEXT NOT NULL DEFAULT '',
    reference_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_invoices_type ON invoices(type);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(invoice_status);
CREATE INDEX IF NOT EXISTS idx_invoices_partner ON invoices(partner_type, partner_id);
CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date);
CREATE INDEX IF NOT EXISTS idx_invoices_company ON invoices(company_id);

-- 对账
CREATE TABLE IF NOT EXISTS reconciliations (
    id TEXT PRIMARY KEY,
    reconciliation_no TEXT UNIQUE NOT NULL,
    partner_type TEXT NOT NULL CHECK (partner_type IN ('customer', 'supplier')),
    partner_id TEXT NOT NULL,
    partner_name TEXT NOT NULL DEFAULT '',
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    order_total NUMERIC NOT NULL DEFAULT 0,
    payment_total NUMERIC NOT NULL DEFAULT 0,
    discrepancy NUMERIC NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_reconciliations_partner ON reconciliations(partner_type, partner_id);
CREATE INDEX IF NOT EXISTS idx_reconciliations_status ON reconciliations(status);
CREATE INDEX IF NOT EXISTS idx_reconciliations_company ON reconciliations(company_id);

CREATE TABLE IF NOT EXISTS reconciliation_items (
    id TEXT PRIMARY KEY,
    reconciliation_id TEXT NOT NULL REFERENCES reconciliations(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL CHECK (item_type IN ('order', 'payment')),
    reference_no TEXT NOT NULL DEFAULT '',
    amount NUMERIC NOT NULL DEFAULT 0,
    reference_date TEXT
);
CREATE INDEX IF NOT EXISTS idx_reconciliation_items_parent ON reconciliation_items(reconciliation_id);

-- ============ 价格体系 ============

CREATE TABLE IF NOT EXISTS product_price_tiers (
    id TEXT PRIMARY KEY,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    min_quantity INTEGER NOT NULL DEFAULT 1,
    max_quantity INTEGER,
    unit_price NUMERIC NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_price_tiers_product ON product_price_tiers(product_id);

CREATE TABLE IF NOT EXISTS customer_product_prices (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    unit_price NUMERIC NOT NULL,
    effective_from TEXT,
    effective_to TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default',
    UNIQUE (customer_id, product_id)
);
CREATE INDEX IF NOT EXISTS idx_customer_price_lookup ON customer_product_prices(customer_id, product_id);

-- ============ 总账 ============

CREATE TABLE IF NOT EXISTS gl_settings (
    company_id TEXT PRIMARY KEY DEFAULT 'default',
    fiscal_year_start INTEGER NOT NULL DEFAULT 1,
    cash_account TEXT NOT NULL DEFAULT '1001',
    bank_account TEXT NOT NULL DEFAULT '1002',
    ar_account TEXT NOT NULL DEFAULT '1122',
    ap_account TEXT NOT NULL DEFAULT '2202',
    inventory_account TEXT NOT NULL DEFAULT '1405',
    revenue_account TEXT NOT NULL DEFAULT '6001',
    cogs_account TEXT NOT NULL DEFAULT '6401',
    opex_account TEXT NOT NULL DEFAULT '6602',
    payroll_account TEXT NOT NULL DEFAULT '2211',
    income_summary TEXT NOT NULL DEFAULT '4103',
    retained_earnings TEXT NOT NULL DEFAULT '410401',
    surplus_account TEXT NOT NULL DEFAULT '1901',
    output_tax_account TEXT NOT NULL DEFAULT '22210105',
    input_tax_account TEXT NOT NULL DEFAULT '22210101',
    auto_post INTEGER NOT NULL DEFAULT TRUE,
    costing_method TEXT NOT NULL DEFAULT 'moving_avg',
    require_review INTEGER NOT NULL DEFAULT FALSE,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS gl_accounts (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    parent_code TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL CHECK (category IN ('asset','liability','equity','cost','income','expense')),
    normal_side TEXT NOT NULL CHECK (normal_side IN ('debit','credit')),
    is_leaf INTEGER NOT NULL DEFAULT TRUE,
    is_cash INTEGER NOT NULL DEFAULT FALSE,
    aux_ar INTEGER NOT NULL DEFAULT FALSE,
    aux_ap INTEGER NOT NULL DEFAULT FALSE,
    active INTEGER NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    company_id TEXT NOT NULL DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_gl_accounts_parent ON gl_accounts(parent_code);
CREATE INDEX IF NOT EXISTS idx_gl_accounts_category ON gl_accounts(category, sort_order);

CREATE TABLE IF NOT EXISTS gl_periods (
    year INTEGER NOT NULL,
    month INTEGER NOT NULL CHECK (month BETWEEN 1 AND 12),
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    closed_at TEXT,
    closed_by TEXT NOT NULL DEFAULT '',
    company_id TEXT NOT NULL DEFAULT 'default',
    PRIMARY KEY (year, month, company_id)
);

CREATE TABLE IF NOT EXISTS gl_voucher_seq (
    period_key TEXT NOT NULL,
    word TEXT NOT NULL DEFAULT '记',
    last_seq INTEGER NOT NULL DEFAULT 0,
    company_id TEXT NOT NULL DEFAULT 'default',
    PRIMARY KEY (period_key, word, company_id)
);

CREATE TABLE IF NOT EXISTS gl_vouchers (
    id TEXT PRIMARY KEY,
    voucher_no TEXT NOT NULL,
    word TEXT NOT NULL DEFAULT '记',
    voucher_date TEXT NOT NULL,
    period_year INTEGER NOT NULL,
    period_month INTEGER NOT NULL,
    attachment_count INTEGER NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','reviewed','posted','void')),
    source_type TEXT NOT NULL DEFAULT '',
    source_id TEXT,
    prepared_by TEXT NOT NULL DEFAULT '',
    posted_at TEXT,
    posted_by TEXT NOT NULL DEFAULT '',
    reverses_id TEXT,
    reversed_by_id TEXT,
    debit_total NUMERIC NOT NULL DEFAULT 0,
    credit_total NUMERIC NOT NULL DEFAULT 0,
    reviewed_by TEXT NOT NULL DEFAULT '',
    reviewed_at TEXT,
    review_note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    company_id TEXT NOT NULL DEFAULT 'default',
    UNIQUE (voucher_no, company_id)
);
CREATE INDEX IF NOT EXISTS idx_gl_vouchers_period ON gl_vouchers(period_year, period_month, status);
CREATE INDEX IF NOT EXISTS idx_gl_vouchers_date ON gl_vouchers(voucher_date);
CREATE INDEX IF NOT EXISTS idx_gl_vouchers_source ON gl_vouchers(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_gl_vouchers_status ON gl_vouchers(status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gl_vouchers_source_unique
    ON gl_vouchers(source_type, source_id)
    WHERE source_id IS NOT NULL AND reverses_id IS NULL AND reversed_by_id IS NULL AND status IN ('draft','posted') AND source_type <> '';

CREATE TABLE IF NOT EXISTS gl_voucher_lines (
    id TEXT PRIMARY KEY,
    voucher_id TEXT NOT NULL REFERENCES gl_vouchers(id) ON DELETE CASCADE,
    line_no INTEGER NOT NULL,
    account_code TEXT NOT NULL REFERENCES gl_accounts(code),
    summary TEXT NOT NULL DEFAULT '',
    debit NUMERIC NOT NULL DEFAULT 0 CHECK (debit >= 0),
    credit NUMERIC NOT NULL DEFAULT 0 CHECK (credit >= 0),
    partner_type TEXT NOT NULL DEFAULT '',
    partner_id TEXT,
    partner_name TEXT NOT NULL DEFAULT '',
    CHECK (NOT (debit > 0 AND credit > 0)),
    CHECK (debit > 0 OR credit > 0),
    UNIQUE (voucher_id, line_no)
);
CREATE INDEX IF NOT EXISTS idx_gl_lines_account ON gl_voucher_lines(account_code);
CREATE INDEX IF NOT EXISTS idx_gl_lines_voucher ON gl_voucher_lines(voucher_id);

CREATE TABLE IF NOT EXISTS gl_account_balances (
    period_year INTEGER NOT NULL,
    period_month INTEGER NOT NULL,
    account_code TEXT NOT NULL REFERENCES gl_accounts(code),
    opening_debit NUMERIC NOT NULL DEFAULT 0,
    opening_credit NUMERIC NOT NULL DEFAULT 0,
    period_debit NUMERIC NOT NULL DEFAULT 0,
    period_credit NUMERIC NOT NULL DEFAULT 0,
    company_id TEXT NOT NULL DEFAULT 'default',
    PRIMARY KEY (period_year, period_month, account_code, company_id)
);

-- ============ 计算列触发器（Turso 不支持 STORED 生成列） ============
-- 体内只写本列、UPDATE 触发器限定 OF(依赖列)，不会自递归。

CREATE TRIGGER IF NOT EXISTS trg_sales_order_items_amount_gen_ai AFTER INSERT ON sales_order_items BEGIN UPDATE sales_order_items SET amount = (quantity * unit_price) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_sales_order_items_amount_gen_au AFTER UPDATE OF quantity, unit_price ON sales_order_items BEGIN UPDATE sales_order_items SET amount = (quantity * unit_price) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_purchase_order_items_amount_gen_ai AFTER INSERT ON purchase_order_items BEGIN UPDATE purchase_order_items SET amount = (quantity * unit_price) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_purchase_order_items_amount_gen_au AFTER UPDATE OF quantity, unit_price ON purchase_order_items BEGIN UPDATE purchase_order_items SET amount = (quantity * unit_price) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_invoices_total_amount_gen_ai AFTER INSERT ON invoices BEGIN UPDATE invoices SET total_amount = (amount + tax_amount) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_invoices_total_amount_gen_au AFTER UPDATE OF amount, tax_amount ON invoices BEGIN UPDATE invoices SET total_amount = (amount + tax_amount) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_products_is_low_stock_gen_ai AFTER INSERT ON products BEGIN UPDATE products SET is_low_stock = (COALESCE(current_stock, 0) <= safety_stock) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_products_is_low_stock_gen_au AFTER UPDATE OF current_stock, safety_stock ON products BEGIN UPDATE products SET is_low_stock = (COALESCE(current_stock, 0) <= safety_stock) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_products_search_text_gen_ai AFTER INSERT ON products BEGIN UPDATE products SET search_text = (name || ' ' || COALESCE(code, '')) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS trg_products_search_text_gen_au AFTER UPDATE OF name, code ON products BEGIN UPDATE products SET search_text = (name || ' ' || COALESCE(code, '')) WHERE rowid = NEW.rowid; END;

-- ============ 业务索引（最终形态） ============

CREATE INDEX IF NOT EXISTS idx_sales_orders_customer ON sales_orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_sales_orders_date ON sales_orders(order_date);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_supplier ON purchase_orders(supplier_id);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_product ON inventory_movements(product_id);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_created ON inventory_movements(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_product_created ON inventory_movements(product_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_date ON payments(payment_date);
CREATE INDEX IF NOT EXISTS idx_payments_type_date ON payments(type, payment_date DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_client_token ON payments(client_token) WHERE client_token IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_products_created_at ON products (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_products_created_id ON products(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_products_low_stock ON products(is_low_stock) WHERE is_low_stock = true;
CREATE INDEX IF NOT EXISTS idx_sales_order_items_order ON sales_order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_purchase_order_items_order ON purchase_order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_batches_product ON inventory_batches(product_id, warehouse_id);
CREATE INDEX IF NOT EXISTS idx_batches_expiry ON inventory_batches(expiry_date) WHERE status = 'normal';

-- ============ 种子数据（幂等，可重复执行） ============

INSERT INTO gl_settings (company_id) VALUES ('default') ON CONFLICT (company_id) DO NOTHING;

INSERT INTO gl_accounts (code, name, parent_code, category, normal_side, is_leaf, is_cash, aux_ar, aux_ap, sort_order) VALUES
('1001', '库存现金', '', 'asset', 'debit', TRUE, TRUE, FALSE, FALSE, 1001),
('1002', '银行存款', '', 'asset', 'debit', TRUE, TRUE, FALSE, FALSE, 1002),
('1012', '其他货币资金', '', 'asset', 'debit', TRUE, TRUE, FALSE, FALSE, 1012),
('1121', '应收票据', '', 'asset', 'debit', TRUE, FALSE, TRUE, FALSE, 1121),
('1122', '应收账款', '', 'asset', 'debit', TRUE, FALSE, TRUE, FALSE, 1122),
('1123', '预付账款', '', 'asset', 'debit', TRUE, FALSE, FALSE, TRUE, 1123),
('1221', '其他应收款', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1221),
('1403', '原材料', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1403),
('1405', '库存商品', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1405),
('1601', '固定资产', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1601),
('1602', '累计折旧', '', 'asset', 'credit', TRUE, FALSE, FALSE, FALSE, 1602),
('1701', '无形资产', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1701),
('1801', '长期待摊费用', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1801),
('1901', '待处理财产损溢', '', 'asset', 'debit', TRUE, FALSE, FALSE, FALSE, 1901),
('2001', '短期借款', '', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 2001),
('2201', '应付票据', '', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 2201),
('2202', '应付账款', '', 'liability', 'credit', TRUE, FALSE, FALSE, TRUE, 2202),
('2203', '预收账款', '', 'liability', 'credit', TRUE, FALSE, TRUE, FALSE, 2203),
('2211', '应付职工薪酬', '', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 2211),
('2221', '应交税费', '', 'liability', 'credit', FALSE, FALSE, FALSE, FALSE, 2221),
('222101', '应交增值税', '2221', 'liability', 'credit', FALSE, FALSE, FALSE, FALSE, 222101),
('22210101', '进项税额', '222101', 'liability', 'debit', TRUE, FALSE, FALSE, FALSE, 22210101),
('22210105', '销项税额', '222101', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 22210105),
('222102', '未交增值税', '2221', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 222102),
('222103', '应交所得税', '2221', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 222103),
('2241', '其他应付款', '', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 2241),
('2501', '长期借款', '', 'liability', 'credit', TRUE, FALSE, FALSE, FALSE, 2501),
('4001', '实收资本', '', 'equity', 'credit', TRUE, FALSE, FALSE, FALSE, 4001),
('4002', '资本公积', '', 'equity', 'credit', TRUE, FALSE, FALSE, FALSE, 4002),
('4101', '盈余公积', '', 'equity', 'credit', TRUE, FALSE, FALSE, FALSE, 4101),
('4103', '本年利润', '', 'equity', 'credit', TRUE, FALSE, FALSE, FALSE, 4103),
('4104', '利润分配', '', 'equity', 'credit', FALSE, FALSE, FALSE, FALSE, 4104),
('410401', '未分配利润', '4104', 'equity', 'credit', TRUE, FALSE, FALSE, FALSE, 410401),
('6001', '主营业务收入', '', 'income', 'credit', TRUE, FALSE, FALSE, FALSE, 6001),
('6051', '其他业务收入', '', 'income', 'credit', TRUE, FALSE, FALSE, FALSE, 6051),
('6301', '营业外收入', '', 'income', 'credit', TRUE, FALSE, FALSE, FALSE, 6301),
('6401', '主营业务成本', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6401),
('6402', '其他业务成本', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6402),
('6403', '税金及附加', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6403),
('6601', '销售费用', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6601),
('6602', '管理费用', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6602),
('6603', '财务费用', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6603),
('6711', '营业外支出', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6711),
('6801', '所得税费用', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6801)
ON CONFLICT (code) DO NOTHING;

INSERT INTO gl_periods (year, month, start_date, end_date, status) VALUES (2024, 1, '2024-01-01', '2024-01-31', 'open'), (2024, 2, '2024-02-01', '2024-02-29', 'open'), (2024, 3, '2024-03-01', '2024-03-31', 'open'), (2024, 4, '2024-04-01', '2024-04-30', 'open'), (2024, 5, '2024-05-01', '2024-05-31', 'open'), (2024, 6, '2024-06-01', '2024-06-30', 'open'), (2024, 7, '2024-07-01', '2024-07-31', 'open'), (2024, 8, '2024-08-01', '2024-08-31', 'open'), (2024, 9, '2024-09-01', '2024-09-30', 'open'), (2024, 10, '2024-10-01', '2024-10-31', 'open'), (2024, 11, '2024-11-01', '2024-11-30', 'open'), (2024, 12, '2024-12-01', '2024-12-31', 'open'), (2025, 1, '2025-01-01', '2025-01-31', 'open'), (2025, 2, '2025-02-01', '2025-02-28', 'open'), (2025, 3, '2025-03-01', '2025-03-31', 'open'), (2025, 4, '2025-04-01', '2025-04-30', 'open'), (2025, 5, '2025-05-01', '2025-05-31', 'open'), (2025, 6, '2025-06-01', '2025-06-30', 'open'), (2025, 7, '2025-07-01', '2025-07-31', 'open'), (2025, 8, '2025-08-01', '2025-08-31', 'open'), (2025, 9, '2025-09-01', '2025-09-30', 'open'), (2025, 10, '2025-10-01', '2025-10-31', 'open'), (2025, 11, '2025-11-01', '2025-11-30', 'open'), (2025, 12, '2025-12-01', '2025-12-31', 'open'), (2026, 1, '2026-01-01', '2026-01-31', 'open'), (2026, 2, '2026-02-01', '2026-02-28', 'open'), (2026, 3, '2026-03-01', '2026-03-31', 'open'), (2026, 4, '2026-04-01', '2026-04-30', 'open'), (2026, 5, '2026-05-01', '2026-05-31', 'open'), (2026, 6, '2026-06-01', '2026-06-30', 'open'), (2026, 7, '2026-07-01', '2026-07-31', 'open'), (2026, 8, '2026-08-01', '2026-08-31', 'open'), (2026, 9, '2026-09-01', '2026-09-30', 'open'), (2026, 10, '2026-10-01', '2026-10-31', 'open'), (2026, 11, '2026-11-01', '2026-11-30', 'open'), (2026, 12, '2026-12-01', '2026-12-31', 'open'), (2027, 1, '2027-01-01', '2027-01-31', 'open'), (2027, 2, '2027-02-01', '2027-02-28', 'open'), (2027, 3, '2027-03-01', '2027-03-31', 'open'), (2027, 4, '2027-04-01', '2027-04-30', 'open'), (2027, 5, '2027-05-01', '2027-05-31', 'open'), (2027, 6, '2027-06-01', '2027-06-30', 'open'), (2027, 7, '2027-07-01', '2027-07-31', 'open'), (2027, 8, '2027-08-01', '2027-08-31', 'open'), (2027, 9, '2027-09-01', '2027-09-30', 'open'), (2027, 10, '2027-10-01', '2027-10-31', 'open'), (2027, 11, '2027-11-01', '2027-11-30', 'open'), (2027, 12, '2027-12-01', '2027-12-31', 'open'), (2028, 1, '2028-01-01', '2028-01-31', 'open'), (2028, 2, '2028-02-01', '2028-02-29', 'open'), (2028, 3, '2028-03-01', '2028-03-31', 'open'), (2028, 4, '2028-04-01', '2028-04-30', 'open'), (2028, 5, '2028-05-01', '2028-05-31', 'open'), (2028, 6, '2028-06-01', '2028-06-30', 'open'), (2028, 7, '2028-07-01', '2028-07-31', 'open'), (2028, 8, '2028-08-01', '2028-08-31', 'open'), (2028, 9, '2028-09-01', '2028-09-30', 'open'), (2028, 10, '2028-10-01', '2028-10-31', 'open'), (2028, 11, '2028-11-01', '2028-11-30', 'open'), (2028, 12, '2028-12-01', '2028-12-31', 'open')
ON CONFLICT (year, month, company_id) DO NOTHING;
