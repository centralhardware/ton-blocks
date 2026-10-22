-- smaller columns: Delta+ZSTD on the values that only grow within an account
-- (rows are sorted by account, lt), ZSTD instead of the default LZ4 on the
-- other numbers. New parts use it at once, old ones as they merge.
-- Buffers are dropped and recreated around the change, as for any ALTER here.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP TABLE IF EXISTS ton.out_messages_buffer;

ALTER TABLE ton.out_messages
    MODIFY COLUMN now DateTime('UTC') CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN mc_seqno UInt32 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN lt UInt64 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN idx UInt16 CODEC(ZSTD(3)),
    MODIFY COLUMN value UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN op Nullable(UInt32) CODEC(ZSTD(3)),
    MODIFY COLUMN bounce UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN bounced UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN fwd_fee UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN created_lt UInt64 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN ihr_fee UInt64 CODEC(ZSTD(3));

ALTER TABLE ton.transactions
    MODIFY COLUMN now DateTime('UTC') CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN mc_seqno UInt32 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN workchain Int8 CODEC(ZSTD(3)),
    MODIFY COLUMN block_seqno UInt32 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN lt UInt64 CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN value UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN op Nullable(UInt32) CODEC(ZSTD(3)),
    MODIFY COLUMN jetton_amount Nullable(UInt128) CODEC(ZSTD(3)),
    MODIFY COLUMN total_fees UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN storage_fees UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN gas_fees UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN fwd_fees UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN action_fees UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN gas_used UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN vm_steps UInt32 CODEC(ZSTD(3)),
    MODIFY COLUMN bounce UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN bounced UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN success UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN exit_code Nullable(Int32) CODEC(ZSTD(3)),
    MODIFY COLUMN action_code Nullable(Int32) CODEC(ZSTD(3)),
    MODIFY COLUMN out_msgs UInt16 CODEC(ZSTD(3)),
    MODIFY COLUMN destroyed UInt8 CODEC(ZSTD(3)),
    MODIFY COLUMN gas_limit UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN gas_credit UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN exit_arg Nullable(Int32) CODEC(ZSTD(3)),
    MODIFY COLUMN total_actions UInt16 CODEC(ZSTD(3)),
    MODIFY COLUMN skipped_actions UInt16 CODEC(ZSTD(3)),
    MODIFY COLUMN msgs_created UInt16 CODEC(ZSTD(3)),
    MODIFY COLUMN msg_cells UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN msg_bits UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN ihr_fee UInt64 CODEC(ZSTD(3)),
    MODIFY COLUMN in_created_lt UInt64 CODEC(Delta, ZSTD(3));

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);
