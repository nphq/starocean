-- 最小 RBAC：admin（改单）/ viewer（只看账）。老用户全部回填 admin，
-- 新用户默认 viewer（fail-closed）；seed 的 admin 显式写入 admin。
ALTER TABLE users ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'viewer';
UPDATE users SET role = 'admin';
