-- per-minute, per-workchain summary, kept forever; filled at insert by minute_mv.
-- Read the uniq columns with uniqMerge.
CREATE TABLE IF NOT EXISTS ton.minute (
    minute           DateTime('UTC'),
    workchain        Int8,
    blocks           AggregateFunction(uniq, UInt32),
    txs              SimpleAggregateFunction(sum, UInt64),
    failed           SimpleAggregateFunction(sum, UInt64),
    external_in      SimpleAggregateFunction(sum, UInt64),
    volume           SimpleAggregateFunction(sum, UInt64),
    fees             SimpleAggregateFunction(sum, UInt64),
    out_msgs         SimpleAggregateFunction(sum, UInt64),
    accounts         AggregateFunction(uniq, String),
    senders          AggregateFunction(uniq, String),
    created          SimpleAggregateFunction(sum, UInt64),
    deployed         SimpleAggregateFunction(sum, UInt64),
    frozen           SimpleAggregateFunction(sum, UInt64),
    deleted          SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(minute)
ORDER BY (workchain, minute);

CREATE MATERIALIZED VIEW IF NOT EXISTS ton.minute_mv TO ton.minute AS
SELECT
    toStartOfMinute(now)              AS minute,
    workchain,
    uniqState(block_seqno)            AS blocks,
    toUInt64(count())                 AS txs,
    toUInt64(countIf(success = 0))    AS failed,
    toUInt64(countIf(in_msg_type = 'externalin')) AS external_in,
    sum(value)                        AS volume,
    sum(total_fees)                   AS fees,
    toUInt64(sum(out_msgs))           AS out_msgs,
    uniqState(account)                AS accounts,
    uniqState(src)                    AS senders,
    -- account lifecycle, from the status before/after each transaction
    toUInt64(countIf(orig_status = 'nonexist' AND end_status != 'nonexist')) AS created,
    toUInt64(countIf(orig_status != 'active' AND end_status = 'active'))     AS deployed,
    toUInt64(countIf(orig_status != 'frozen' AND end_status = 'frozen'))     AS frozen,
    toUInt64(countIf(orig_status != 'nonexist' AND end_status = 'nonexist')) AS deleted
FROM ton.transactions
GROUP BY minute, workchain;
