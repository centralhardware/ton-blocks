-- Drop each transaction's own hash: 3.2 GiB, the largest column by far (a
-- random 32-byte value doesn't compress), and nothing reads it. trace_id /
-- trace_seq still group a user action; the explorers' id is the trace_id.
-- Buffers can't ALTER: dropped and recreated.
DROP TABLE IF EXISTS ton.transactions_buffer;

ALTER TABLE ton.transactions DROP COLUMN IF EXISTS hash;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
