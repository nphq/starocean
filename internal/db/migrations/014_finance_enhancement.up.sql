CREATE TABLE reimbursements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reimbursement_no VARCHAR(20) UNIQUE NOT NULL,
    applicant_name VARCHAR(200) NOT NULL,
    department VARCHAR(200) NOT NULL DEFAULT '',
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    category VARCHAR(50) NOT NULL DEFAULT '其他',
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected', 'paid')),
    approver_name VARCHAR(200) NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ,
    rejected_reason TEXT NOT NULL DEFAULT '',
    payment_id UUID,
    expense_date DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_reimbursements_status ON reimbursements(status);
CREATE INDEX idx_reimbursements_applicant ON reimbursements(applicant_name);
CREATE INDEX idx_reimbursements_date ON reimbursements(expense_date);
CREATE INDEX idx_reimbursements_company ON reimbursements(company_id);

CREATE TABLE reimbursement_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reimbursement_id UUID NOT NULL REFERENCES reimbursements(id) ON DELETE CASCADE,
    category VARCHAR(50) NOT NULL DEFAULT '其他',
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_reimbursement_items_parent ON reimbursement_items(reimbursement_id);

CREATE TABLE invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no VARCHAR(50) UNIQUE NOT NULL,
    type VARCHAR(10) NOT NULL CHECK (type IN ('input', 'output')),
    partner_type VARCHAR(10) NOT NULL CHECK (partner_type IN ('customer', 'supplier')),
    partner_id UUID,
    partner_name VARCHAR(200) NOT NULL DEFAULT '',
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_rate DECIMAL(5,2) NOT NULL DEFAULT 0,
    tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    total_amount DECIMAL(15,2) GENERATED ALWAYS AS (amount + tax_amount) STORED,
    invoice_date DATE,
    invoice_code VARCHAR(50) NOT NULL DEFAULT '',
    invoice_status VARCHAR(20) NOT NULL DEFAULT 'normal' CHECK (invoice_status IN ('normal', 'void', 'red')),
    reference_type VARCHAR(30) NOT NULL DEFAULT '',
    reference_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_invoices_type ON invoices(type);
CREATE INDEX idx_invoices_status ON invoices(invoice_status);
CREATE INDEX idx_invoices_partner ON invoices(partner_type, partner_id);
CREATE INDEX idx_invoices_date ON invoices(invoice_date);
CREATE INDEX idx_invoices_company ON invoices(company_id);

CREATE TABLE reconciliations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_no VARCHAR(20) UNIQUE NOT NULL,
    partner_type VARCHAR(10) NOT NULL CHECK (partner_type IN ('customer', 'supplier')),
    partner_id UUID NOT NULL,
    partner_name VARCHAR(200) NOT NULL DEFAULT '',
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    order_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    payment_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    discrepancy DECIMAL(15,2) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_reconciliations_partner ON reconciliations(partner_type, partner_id);
CREATE INDEX idx_reconciliations_status ON reconciliations(status);
CREATE INDEX idx_reconciliations_company ON reconciliations(company_id);

CREATE TABLE reconciliation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id UUID NOT NULL REFERENCES reconciliations(id) ON DELETE CASCADE,
    item_type VARCHAR(20) NOT NULL CHECK (item_type IN ('order', 'payment')),
    reference_no VARCHAR(50) NOT NULL DEFAULT '',
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    reference_date DATE
);

CREATE INDEX idx_reconciliation_items_parent ON reconciliation_items(reconciliation_id);

CREATE SEQUENCE IF NOT EXISTS reimbursement_seq;
CREATE SEQUENCE IF NOT EXISTS reconciliation_seq;
