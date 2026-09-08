-- 025_voucher_review 回滚（PostgreSQL golang-migrate 使用；SQLite 迁移只读 .up.sql）。
ALTER TABLE gl_vouchers DROP COLUMN review_note;
ALTER TABLE gl_vouchers DROP COLUMN reviewed_at;
ALTER TABLE gl_vouchers DROP COLUMN reviewed_by;
ALTER TABLE gl_settings DROP COLUMN require_review;

-- 状态值域退回 draft/posted/void
ALTER TABLE gl_vouchers DROP CONSTRAINT gl_vouchers_status_check;
ALTER TABLE gl_vouchers ADD CONSTRAINT gl_vouchers_status_check
    CHECK (status IN ('draft','posted','void'));
