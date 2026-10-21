-- one slim row per transaction, dropped after 90 days. trace_id: hash of the
-- transaction that started the trace (0 = unknown), see README "Traces".
CREATE TABLE IF NOT EXISTS ton.transactions (
    now              DateTime('UTC'),
    mc_seqno         UInt32,
    workchain        Int8,
    block_seqno      UInt32,
    account          String CODEC(ZSTD(3)),
    lt               UInt64,
    trace_id         UInt256,
    in_msg_type      LowCardinality(String),
    src              String CODEC(ZSTD(3)),
    value            UInt64,
    op               Nullable(UInt32),
    total_fees       UInt64,
    success          UInt8,
    exit_code        Nullable(Int32),
    out_msgs         UInt16,
    orig_status      LowCardinality(String),
    end_status       LowCardinality(String)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(now)
ORDER BY (account, lt)
TTL now + INTERVAL 90 DAY;
