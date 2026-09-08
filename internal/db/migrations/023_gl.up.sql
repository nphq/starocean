CREATE TABLE gl_settings (
    company_id VARCHAR(50) PRIMARY KEY DEFAULT 'default',
    fiscal_year_start SMALLINT NOT NULL DEFAULT 1,
    cash_account VARCHAR(20) NOT NULL DEFAULT '1001',
    bank_account VARCHAR(20) NOT NULL DEFAULT '1002',
    ar_account VARCHAR(20) NOT NULL DEFAULT '1122',
    ap_account VARCHAR(20) NOT NULL DEFAULT '2202',
    inventory_account VARCHAR(20) NOT NULL DEFAULT '1405',
    revenue_account VARCHAR(20) NOT NULL DEFAULT '6001',
    cogs_account VARCHAR(20) NOT NULL DEFAULT '6401',
    opex_account VARCHAR(20) NOT NULL DEFAULT '6602',
    payroll_account VARCHAR(20) NOT NULL DEFAULT '2211',
    income_summary VARCHAR(20) NOT NULL DEFAULT '4103',
    retained_earnings VARCHAR(20) NOT NULL DEFAULT '410401',
    surplus_account VARCHAR(20) NOT NULL DEFAULT '1901',
    auto_post BOOLEAN NOT NULL DEFAULT TRUE,
    costing_method VARCHAR(20) NOT NULL DEFAULT 'moving_avg',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO gl_settings (company_id) VALUES ('default');

CREATE TABLE gl_accounts (
    code VARCHAR(20) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    parent_code VARCHAR(20) NOT NULL DEFAULT '',
    category VARCHAR(20) NOT NULL CHECK (category IN ('asset','liability','equity','cost','income','expense')),
    normal_side VARCHAR(6) NOT NULL CHECK (normal_side IN ('debit','credit')),
    is_leaf BOOLEAN NOT NULL DEFAULT TRUE,
    is_cash BOOLEAN NOT NULL DEFAULT FALSE,
    aux_ar BOOLEAN NOT NULL DEFAULT FALSE,
    aux_ap BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_gl_accounts_parent ON gl_accounts(parent_code);
CREATE INDEX idx_gl_accounts_category ON gl_accounts(category, sort_order);

CREATE TABLE gl_periods (
    year INT NOT NULL,
    month INT NOT NULL CHECK (month BETWEEN 1 AND 12),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status VARCHAR(10) NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    closed_at TIMESTAMPTZ,
    closed_by VARCHAR(100) NOT NULL DEFAULT '',
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    PRIMARY KEY (year, month, company_id)
);

CREATE TABLE gl_voucher_seq (
    period_key VARCHAR(10) NOT NULL,
    word VARCHAR(8) NOT NULL DEFAULT '记',
    last_seq INT NOT NULL DEFAULT 0,
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    PRIMARY KEY (period_key, word, company_id)
);

CREATE TABLE gl_vouchers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voucher_no VARCHAR(30) NOT NULL,
    word VARCHAR(8) NOT NULL DEFAULT '记',
    voucher_date DATE NOT NULL,
    period_year INT NOT NULL,
    period_month INT NOT NULL,
    attachment_count INT NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    status VARCHAR(10) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','posted','void')),
    source_type VARCHAR(40) NOT NULL DEFAULT '',
    source_id UUID,
    prepared_by VARCHAR(100) NOT NULL DEFAULT '',
    posted_at TIMESTAMPTZ,
    posted_by VARCHAR(100) NOT NULL DEFAULT '',
    reverses_id UUID,
    reversed_by_id UUID,
    debit_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    credit_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    UNIQUE (voucher_no, company_id)
);

CREATE INDEX idx_gl_vouchers_period ON gl_vouchers(period_year, period_month, status);
CREATE INDEX idx_gl_vouchers_date ON gl_vouchers(voucher_date);
CREATE INDEX idx_gl_vouchers_source ON gl_vouchers(source_type, source_id);
CREATE INDEX idx_gl_vouchers_status ON gl_vouchers(status);

CREATE UNIQUE INDEX idx_gl_vouchers_source_unique
    ON gl_vouchers(source_type, source_id)
    WHERE source_id IS NOT NULL AND reverses_id IS NULL AND reversed_by_id IS NULL AND status IN ('draft','posted') AND source_type <> '';

CREATE TABLE gl_voucher_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voucher_id UUID NOT NULL REFERENCES gl_vouchers(id) ON DELETE CASCADE,
    line_no INT NOT NULL,
    account_code VARCHAR(20) NOT NULL REFERENCES gl_accounts(code),
    summary TEXT NOT NULL DEFAULT '',
    debit DECIMAL(15,2) NOT NULL DEFAULT 0 CHECK (debit >= 0),
    credit DECIMAL(15,2) NOT NULL DEFAULT 0 CHECK (credit >= 0),
    partner_type VARCHAR(20) NOT NULL DEFAULT '',
    partner_id UUID,
    partner_name VARCHAR(200) NOT NULL DEFAULT '',
    CHECK (NOT (debit > 0 AND credit > 0)),
    CHECK (debit > 0 OR credit > 0),
    UNIQUE (voucher_id, line_no)
);

CREATE INDEX idx_gl_lines_account ON gl_voucher_lines(account_code);
CREATE INDEX idx_gl_lines_voucher ON gl_voucher_lines(voucher_id);

CREATE TABLE gl_account_balances (
    period_year INT NOT NULL,
    period_month INT NOT NULL,
    account_code VARCHAR(20) NOT NULL REFERENCES gl_accounts(code),
    opening_debit DECIMAL(15,2) NOT NULL DEFAULT 0,
    opening_credit DECIMAL(15,2) NOT NULL DEFAULT 0,
    period_debit DECIMAL(15,2) NOT NULL DEFAULT 0,
    period_credit DECIMAL(15,2) NOT NULL DEFAULT 0,
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    PRIMARY KEY (period_year, period_month, account_code, company_id)
);

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
('6801', '所得税费用', '', 'expense', 'debit', TRUE, FALSE, FALSE, FALSE, 6801);

INSERT INTO gl_periods (year, month, start_date, end_date, status)
SELECT y, m,
       make_date(y, m, 1),
       (make_date(y, m, 1) + INTERVAL '1 month' - INTERVAL '1 day')::date,
       'open'
FROM generate_series(2024, 2028) AS y,
     generate_series(1, 12) AS m;
