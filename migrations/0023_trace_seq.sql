-- trace_seq: a sequential trace number (1, 2, 3, ... in order of the trace's
-- first transaction) next to the 32-byte trace_id hash, which doesn't compress.
-- The logger hands out the next number when a trace starts. Existing rows are
-- numbered by backfillTraceSeq (traceseq.go) before the logger starts, which
-- then puts a 1-day column TTL on trace_id in transactions and out_messages:
-- after a day only trace_seq is left (tonviewer links need the full hash).
-- 0 = unknown trace, like trace_id 0.
-- swaps and dns_changes get trace_seq too, but keep the full hash: it's in
-- their sort key (no column TTL there), and they're small.
-- Buffers can't ALTER: dropped and recreated.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP TABLE IF EXISTS ton.out_messages_buffer;

ALTER TABLE ton.transactions ADD COLUMN IF NOT EXISTS trace_seq UInt64 CODEC(Delta, ZSTD(3)) AFTER trace_id;
ALTER TABLE ton.out_messages ADD COLUMN IF NOT EXISTS trace_seq UInt64 CODEC(Delta, ZSTD(3)) AFTER trace_id;
ALTER TABLE ton.swaps ADD COLUMN IF NOT EXISTS trace_seq UInt64 CODEC(Delta, ZSTD(3)) AFTER trace_id;
ALTER TABLE ton.dns_changes ADD COLUMN IF NOT EXISTS trace_seq UInt64 CODEC(Delta, ZSTD(3)) AFTER trace_id;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);
