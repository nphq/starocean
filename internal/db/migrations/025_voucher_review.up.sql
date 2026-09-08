-- 025_voucher_review: 凭证制单/审核职责分离（reviewed 状态 + 审核人 + 开关）
--
-- PostgreSQL 与 SQLite 双方言要求：
--   * 新增列用 ALTER TABLE ADD COLUMN（两种方言均支持）。
--   * 状态值域需增加 'reviewed'，但 SQLite 无法 ALTER 列级 CHECK，须整表重建。
--     为避免 SQLite 外键 ON DELETE CASCADE 在 DROP gl_vouchers 时级联清空 gl_voucher_lines，
--     先备份子表、删除子表，再重建父表，最后重建子表并回填，最后删除备份。
--     该序列为标准 SQL，在 PostgreSQL 与 SQLite 上等价可执行，故本文件双方言共用。

ALTER TABLE gl_vouchers ADD COLUMN reviewed_by VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE gl_vouchers ADD COLUMN reviewed_at TIMESTAMPTZ;
ALTER TABLE gl_vouchers ADD COLUMN review_note TEXT NOT NULL DEFAULT '';
ALTER TABLE gl_settings ADD COLUMN require_review BOOLEAN NOT NULL DEFAULT FALSE;

-- 备份子表数据（无约束的临时表，避免级联）
CREATE TABLE gl_voucher_lines_backup AS SELECT * FROM gl_voucher_lines;
DROP TABLE gl_voucher_lines;

-- 重建 gl_vouchers：加入 reviewed_by/reviewed_at/review_note，状态值域扩展为 draft/reviewed/posted/void
CREATE TABLE gl_vouchers_new (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voucher_no VARCHAR(30) NOT NULL,
    word VARCHAR(8) NOT NULL DEFAULT '记',
    voucher_date DATE NOT NULL,
    period_year INT NOT NULL,
    period_month INT NOT NULL,
    attachment_count INT NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    status VARCHAR(10) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','reviewed','posted','void')),
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
    reviewed_by VARCHAR(100) NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ,
    review_note TEXT NOT NULL DEFAULT '',
    UNIQUE (voucher_no, company_id)
);

INSERT INTO gl_vouchers_new (
    id, voucher_no, word, voucher_date, period_year, period_month, attachment_count,
    summary, status, source_type, source_id, prepared_by, posted_at, posted_by,
    reverses_id, reversed_by_id, debit_total, credit_total, created_at, updated_at,
    company_id, reviewed_by, reviewed_at, review_note
)
SELECT id, voucher_no, word, voucher_date, period_year, period_month, attachment_count,
       summary, status, source_type, source_id, prepared_by, posted_at, posted_by,
       reverses_id, reversed_by_id, debit_total, credit_total, created_at, updated_at,
       company_id, '', NULL, ''
FROM gl_vouchers;

DROP TABLE gl_vouchers;
ALTER TABLE gl_vouchers_new RENAME TO gl_vouchers;

CREATE INDEX idx_gl_vouchers_period ON gl_vouchers(period_year, period_month, status);
CREATE INDEX idx_gl_vouchers_date ON gl_vouchers(voucher_date);
CREATE INDEX idx_gl_vouchers_source ON gl_vouchers(source_type, source_id);
CREATE INDEX idx_gl_vouchers_status ON gl_vouchers(status);
CREATE UNIQUE INDEX idx_gl_vouchers_source_unique
    ON gl_vouchers(source_type, source_id)
    WHERE source_id IS NOT NULL AND reverses_id IS NULL AND reversed_by_id IS NULL AND status IN ('draft','posted') AND source_type <> '';

-- 重建 gl_voucher_lines（含全部约束），并从备份回填
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

INSERT INTO gl_voucher_lines (id, voucher_id, line_no, account_code, summary, debit, credit, partner_type, partner_id, partner_name)
SELECT id, voucher_id, line_no, account_code, summary, debit, credit, partner_type, partner_id, partner_name
FROM gl_voucher_lines_backup;

CREATE INDEX idx_gl_lines_account ON gl_voucher_lines(account_code);
CREATE INDEX idx_gl_lines_voucher ON gl_voucher_lines(voucher_id);

DROP TABLE gl_voucher_lines_backup;
