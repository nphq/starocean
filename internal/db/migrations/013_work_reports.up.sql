CREATE TABLE work_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type VARCHAR(10) NOT NULL CHECK (type IN ('daily', 'weekly')),
    report_date DATE NOT NULL,
    employee_code VARCHAR(50) NOT NULL,
    employee_name VARCHAR(200) NOT NULL,
    department VARCHAR(200) NOT NULL DEFAULT '',
    work_done TEXT NOT NULL DEFAULT '',
    tomorrow_plan TEXT NOT NULL DEFAULT '',
    issues TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_work_reports_date ON work_reports(report_date DESC);
CREATE INDEX idx_work_reports_employee ON work_reports(employee_code);
CREATE INDEX idx_work_reports_type ON work_reports(type);
CREATE INDEX idx_work_reports_status ON work_reports(status);
CREATE INDEX idx_work_reports_company ON work_reports(company_id);

CREATE TABLE weekly_report_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    weekly_report_id UUID NOT NULL REFERENCES work_reports(id) ON DELETE CASCADE,
    daily_report_id UUID NOT NULL REFERENCES work_reports(id) ON DELETE CASCADE,
    UNIQUE(weekly_report_id, daily_report_id)
);

CREATE INDEX idx_weekly_links_weekly ON weekly_report_links(weekly_report_id);
CREATE INDEX idx_weekly_links_daily ON weekly_report_links(daily_report_id);

CREATE TABLE report_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    type VARCHAR(10) NOT NULL CHECK (type IN ('daily', 'weekly')),
    work_done TEXT NOT NULL DEFAULT '',
    tomorrow_plan TEXT NOT NULL DEFAULT '',
    issues TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_report_templates_type ON report_templates(type, company_id);
