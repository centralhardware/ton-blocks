-- hourly counts by op code (-1 = no body, 0 = text comment), kept forever.
-- Hourly, because a single minute already sees ~350 distinct op codes.
CREATE TABLE IF NOT EXISTS ton.ops_hour (
    hour             DateTime('UTC'),
    workchain        Int8,
    op               Int64,
    txs              SimpleAggregateFunction(sum, UInt64),
    failed           SimpleAggregateFunction(sum, UInt64),
    volume           SimpleAggregateFunction(sum, UInt64),
    fees             SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(hour)
ORDER BY (workchain, op, hour);

CREATE MATERIALIZED VIEW IF NOT EXISTS ton.ops_hour_mv TO ton.ops_hour AS
SELECT
    toStartOfHour(now)                AS hour,
    workchain,
    ifNull(toInt64(op), -1)           AS op,
    toUInt64(count())                 AS txs,
    toUInt64(countIf(success = 0))    AS failed,
    sum(value)                        AS volume,
    sum(total_fees)                   AS fees
FROM ton.transactions
GROUP BY hour, workchain, op;
