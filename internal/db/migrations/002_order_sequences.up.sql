-- 002_order_sequences.up.sql
CREATE TABLE order_sequences (
    seq_key VARCHAR(50) PRIMARY KEY,
    last_seq INT NOT NULL DEFAULT 0
);
