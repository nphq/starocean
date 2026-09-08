-- 004_attendance.up.sql
CREATE TABLE attendance_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_code VARCHAR(50) NOT NULL,
    employee_name VARCHAR(200) NOT NULL,
    department VARCHAR(200),
    check_in_time TIMESTAMPTZ NOT NULL,
    check_type VARCHAR(20) NOT NULL DEFAULT 'check_in',
    source VARCHAR(20) NOT NULL DEFAULT 'manual',
    location VARCHAR(200),
    remark TEXT,
    source_raw JSONB,
    check_date DATE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(employee_name, check_in_time)
);

CREATE INDEX idx_attendance_date ON attendance_records (check_date DESC);
CREATE INDEX idx_attendance_source ON attendance_records (source);
CREATE INDEX idx_attendance_emp_date ON attendance_records (employee_name, check_date DESC);
