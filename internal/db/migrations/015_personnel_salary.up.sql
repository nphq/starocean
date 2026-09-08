CREATE TABLE departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    code VARCHAR(20) UNIQUE NOT NULL,
    parent_id UUID REFERENCES departments(id),
    manager_name VARCHAR(200) NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_departments_parent ON departments(parent_id);
CREATE INDEX idx_departments_company ON departments(company_id);

CREATE TABLE positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    department_id UUID REFERENCES departments(id),
    base_salary DECIMAL(15,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_positions_department ON positions(department_id);
CREATE INDEX idx_positions_company ON positions(company_id);

CREATE TABLE employees (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(50) UNIQUE NOT NULL,
    name VARCHAR(200) NOT NULL,
    department_id UUID REFERENCES departments(id),
    position_id UUID REFERENCES positions(id),
    phone VARCHAR(30) NOT NULL DEFAULT '',
    email VARCHAR(100) NOT NULL DEFAULT '',
    id_number VARCHAR(18) NOT NULL DEFAULT '',
    hire_date DATE,
    separation_date DATE,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'probation', 'inactive')),
    emergency_contact VARCHAR(200) NOT NULL DEFAULT '',
    emergency_phone VARCHAR(30) NOT NULL DEFAULT '',
    bank_name VARCHAR(100) NOT NULL DEFAULT '',
    bank_account VARCHAR(50) NOT NULL DEFAULT '',
    properties JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_employees_status ON employees(status);
CREATE INDEX idx_employees_department ON employees(department_id);
CREATE INDEX idx_employees_name ON employees(name);
CREATE INDEX idx_employees_company ON employees(company_id);

CREATE TABLE contracts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    contract_no VARCHAR(50) UNIQUE NOT NULL,
    contract_type VARCHAR(30) NOT NULL DEFAULT '固定期限' CHECK (contract_type IN ('固定期限', '无固定期限', '劳务')),
    start_date DATE,
    end_date DATE,
    salary DECIMAL(15,2) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired', 'terminated')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_contracts_employee ON contracts(employee_id);
CREATE INDEX idx_contracts_end_date ON contracts(end_date);
CREATE INDEX idx_contracts_company ON contracts(company_id);

CREATE TABLE salary_components (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    month DATE NOT NULL,
    base_salary DECIMAL(15,2) NOT NULL DEFAULT 0,
    overtime_pay DECIMAL(15,2) NOT NULL DEFAULT 0,
    bonus DECIMAL(15,2) NOT NULL DEFAULT 0,
    deduction DECIMAL(15,2) NOT NULL DEFAULT 0,
    social_security DECIMAL(15,2) NOT NULL DEFAULT 0,
    housing_fund DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax DECIMAL(15,2) NOT NULL DEFAULT 0,
    net_salary DECIMAL(15,2) GENERATED ALWAYS AS (base_salary + overtime_pay + bonus - deduction - social_security - housing_fund - tax) STORED,
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    UNIQUE(employee_id, month)
);

CREATE INDEX idx_salary_month ON salary_components(month);
CREATE INDEX idx_salary_employee ON salary_components(employee_id);
CREATE INDEX idx_salary_status ON salary_components(status);
CREATE INDEX idx_salary_company ON salary_components(company_id);
