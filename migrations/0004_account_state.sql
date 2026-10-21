-- every account seen in a transaction, with its latest status (argMax by lt), kept
-- forever. Only accounts that transacted since the logger started, not the whole chain.
CREATE TABLE IF NOT EXISTS ton.account_state (
    account          String CODEC(ZSTD(3)),
    workchain        Int8,
    status           AggregateFunction(argMax, String, UInt64),
    first_seen       SimpleAggregateFunction(min, DateTime('UTC')),
    last_seen        SimpleAggregateFunction(max, DateTime('UTC')),
    txs              SimpleAggregateFunction(sum, UInt64),
    failed           SimpleAggregateFunction(sum, UInt64),
    received         SimpleAggregateFunction(sum, UInt64),
    fees             SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
-- workchain is implied by the account, but an AggregatingMergeTree refuses a
-- plain column outside the sorting key
ORDER BY (workchain, account);

CREATE MATERIALIZED VIEW IF NOT EXISTS ton.account_state_mv TO ton.account_state AS

SELECT
    account,
    workchain,
    argMaxState(toString(end_status), lt) AS status,
    min(now)                          AS first_seen,
    max(now)                          AS last_seen,
    toUInt64(count())                 AS txs,
    toUInt64(countIf(success = 0))    AS failed,
    sum(value)                        AS received,
    sum(total_fees)                   AS fees
FROM ton.transactions
GROUP BY account, workchain;

-- the current state, one row per account
CREATE VIEW IF NOT EXISTS ton.accounts AS
SELECT
    account,
    workchain,
    argMaxMerge(status)  AS status,
    min(first_seen)      AS first_seen,
    max(last_seen)       AS last_seen,
    sum(txs)             AS txs,
    sum(failed)          AS failed,
    sum(received)        AS received,
    sum(fees)            AS fees
FROM ton.account_state
GROUP BY account, workchain;

CREATE VIEW IF NOT EXISTS ton.account_status_counts AS
SELECT workchain, status, count() AS accounts
FROM ton.accounts
GROUP BY workchain, status;
