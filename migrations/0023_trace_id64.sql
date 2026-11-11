-- trace_id is a 32-byte hash that doesn't compress. It's only needed in full
-- for tonviewer links, and a trace finishes within minutes, so after a day a
-- row keeps only trace_id64 = its low 8 bytes (= the first 8 bytes in memory),
-- which is enough to group a trace's rows. A collision only matters inside
-- one group-by window. 0 stays 0 (= unknown).
-- Group old data by trace_id64; trace_id reads 0 once its column TTL has run.
-- The column TTL resets trace_id to its default (0) during merges; that
-- compresses to almost nothing. trace_id64 is materialized synchronously
-- first, so no part computes it from an already-zeroed trace_id.
-- swaps and dns_changes keep the full hash: trace_id is in their sort key
-- (a column TTL isn't allowed there), and they're small.
-- Buffers can't ALTER: dropped and recreated.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP TABLE IF EXISTS ton.out_messages_buffer;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS trace_id64 UInt64
        DEFAULT reinterpretAsUInt64(reinterpretAsFixedString(trace_id)) CODEC(ZSTD(3)) AFTER trace_id;
ALTER TABLE ton.transactions MATERIALIZE COLUMN trace_id64 SETTINGS mutations_sync = 2;
ALTER TABLE ton.transactions MODIFY COLUMN trace_id UInt256 TTL now + INTERVAL 1 DAY;

ALTER TABLE ton.out_messages
    ADD COLUMN IF NOT EXISTS trace_id64 UInt64
        DEFAULT reinterpretAsUInt64(reinterpretAsFixedString(trace_id)) CODEC(ZSTD(3)) AFTER trace_id;
ALTER TABLE ton.out_messages MATERIALIZE COLUMN trace_id64 SETTINGS mutations_sync = 2;
ALTER TABLE ton.out_messages MODIFY COLUMN trace_id UInt256 TTL now + INTERVAL 1 DAY;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);
