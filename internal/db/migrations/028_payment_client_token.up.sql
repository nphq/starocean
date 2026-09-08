-- 028_payment_client_token 付款幂等键：同一令牌（表单一次性令牌/客户端去重键）
-- 只允许落一笔流水，防双击/刷新重复提交。部分唯一索引，NULL 不受限。
ALTER TABLE payments ADD COLUMN client_token VARCHAR(64);
CREATE UNIQUE INDEX idx_payments_client_token ON payments(client_token) WHERE client_token IS NOT NULL;
