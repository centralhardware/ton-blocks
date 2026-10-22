-- transactions: monthly partitions instead of daily, like the other tables, so
-- whole months can move to cold storage. A partition key can't be ALTERed, so
-- the table is rebuilt: copy into a new table, swap names, drop the old one.
-- The views are dropped first so the copy doesn't count every row again in
-- minute/ops_hour/account_state, and recreated on the new table (same DDL).
-- Runs at start, before the logger writes anything.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP VIEW IF EXISTS ton.minute_mv;
DROP VIEW IF EXISTS ton.ops_hour_mv;
DROP VIEW IF EXISTS ton.account_state_mv;
DROP TABLE IF EXISTS ton.transactions_monthly;

CREATE TABLE ton.transactions_monthly AS ton.transactions
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(now)
ORDER BY (account, lt);

INSERT INTO ton.transactions_monthly SELECT * FROM ton.transactions;

EXCHANGE TABLES ton.transactions AND ton.transactions_monthly;

DROP TABLE ton.transactions_monthly;

CREATE MATERIALIZED VIEW ton.account_state_mv TO ton.account_state (`account` String, `workchain` Int8, `status` AggregateFunction(argMax, String, UInt64), `first_seen` DateTime('UTC'), `last_seen` DateTime('UTC'), `txs` UInt64, `failed` UInt64, `received` UInt64, `fees` UInt64) AS SELECT account, workchain, argMaxState(toString(end_status), lt) AS status, min(now) AS first_seen, max(now) AS last_seen, toUInt64(count()) AS txs, toUInt64(countIf(success = 0)) AS failed, sum(value) AS received, sum(total_fees) AS fees FROM ton.transactions GROUP BY account, workchain;
CREATE MATERIALIZED VIEW ton.minute_mv TO ton.minute (`minute` DateTime('UTC'), `workchain` Int8, `blocks` AggregateFunction(uniq, UInt32), `txs` UInt64, `failed` UInt64, `external_in` UInt64, `volume` UInt64, `fees` UInt64, `out_msgs` UInt64, `accounts` AggregateFunction(uniq, String), `senders` AggregateFunction(uniq, String), `created` UInt64, `deployed` UInt64, `frozen` UInt64, `deleted` UInt64) AS SELECT toStartOfMinute(now) AS minute, workchain, uniqState(block_seqno) AS blocks, toUInt64(count()) AS txs, toUInt64(countIf(success = 0)) AS failed, toUInt64(countIf(in_msg_type = 'externalin')) AS external_in, sum(value) AS volume, sum(total_fees) AS fees, toUInt64(sum(out_msgs)) AS out_msgs, uniqState(account) AS accounts, uniqState(src) AS senders, toUInt64(countIf((orig_status = 'nonexist') AND (end_status != 'nonexist'))) AS created, toUInt64(countIf((orig_status != 'active') AND (end_status = 'active'))) AS deployed, toUInt64(countIf((orig_status != 'frozen') AND (end_status = 'frozen'))) AS frozen, toUInt64(countIf((orig_status != 'nonexist') AND (end_status = 'nonexist'))) AS deleted FROM ton.transactions GROUP BY minute, workchain;
CREATE MATERIALIZED VIEW ton.ops_hour_mv TO ton.ops_hour (`hour` DateTime('UTC'), `workchain` Int8, `op` Int64, `txs` UInt64, `failed` UInt64, `volume` UInt64, `fees` UInt64) AS SELECT toStartOfHour(now) AS hour, workchain, ifNull(toInt64(op), -1) AS op, toUInt64(count()) AS txs, toUInt64(countIf(success = 0)) AS failed, sum(value) AS volume, sum(total_fees) AS fees FROM ton.transactions GROUP BY hour, workchain, op;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
