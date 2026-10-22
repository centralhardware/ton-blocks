-- the rest of the block data (see blockdata.go):
-- transactions: own hash (UInt256 like trace_id), tx_type (ord, tick, tock,
--   storage, split_*/merge_*; tick-tock transactions are stored from now on,
--   rows before this are ord or one of the rare others, never tick/tock),
--   compute details (gas_limit, gas_credit, exit_arg), action phase counts and
--   message size, destroyed, the incoming message's ihr_fee, created_lt and
--   extra currencies (id -> amount).
-- minute/ops_hour views now skip tick-tock, so their numbers stay comparable.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP VIEW IF EXISTS ton.minute_mv;
DROP VIEW IF EXISTS ton.ops_hour_mv;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS hash UInt256 AFTER trace_id,
    ADD COLUMN IF NOT EXISTS tx_type LowCardinality(String) DEFAULT 'ord' AFTER hash,
    ADD COLUMN IF NOT EXISTS destroyed UInt8,
    ADD COLUMN IF NOT EXISTS gas_limit UInt64,
    ADD COLUMN IF NOT EXISTS gas_credit UInt64,
    ADD COLUMN IF NOT EXISTS exit_arg Nullable(Int32),
    ADD COLUMN IF NOT EXISTS total_actions UInt16,
    ADD COLUMN IF NOT EXISTS skipped_actions UInt16,
    ADD COLUMN IF NOT EXISTS msgs_created UInt16,
    ADD COLUMN IF NOT EXISTS msg_cells UInt64,
    ADD COLUMN IF NOT EXISTS msg_bits UInt64,
    ADD COLUMN IF NOT EXISTS ihr_fee UInt64,
    ADD COLUMN IF NOT EXISTS in_created_lt UInt64,
    ADD COLUMN IF NOT EXISTS extra_currencies Map(UInt32, UInt256);

CREATE MATERIALIZED VIEW ton.minute_mv TO ton.minute (`minute` DateTime('UTC'), `workchain` Int8, `blocks` AggregateFunction(uniq, UInt32), `txs` UInt64, `failed` UInt64, `external_in` UInt64, `volume` UInt64, `fees` UInt64, `out_msgs` UInt64, `accounts` AggregateFunction(uniq, String), `senders` AggregateFunction(uniq, String), `created` UInt64, `deployed` UInt64, `frozen` UInt64, `deleted` UInt64) AS SELECT toStartOfMinute(now) AS minute, workchain, uniqState(block_seqno) AS blocks, toUInt64(count()) AS txs, toUInt64(countIf(success = 0)) AS failed, toUInt64(countIf(in_msg_type = 'externalin')) AS external_in, sum(value) AS volume, sum(total_fees) AS fees, toUInt64(sum(out_msgs)) AS out_msgs, uniqState(account) AS accounts, uniqState(src) AS senders, toUInt64(countIf((orig_status = 'nonexist') AND (end_status != 'nonexist'))) AS created, toUInt64(countIf((orig_status != 'active') AND (end_status = 'active'))) AS deployed, toUInt64(countIf((orig_status != 'frozen') AND (end_status = 'frozen'))) AS frozen, toUInt64(countIf((orig_status != 'nonexist') AND (end_status = 'nonexist'))) AS deleted FROM ton.transactions WHERE tx_type NOT IN ('tick', 'tock') GROUP BY minute, workchain;
CREATE MATERIALIZED VIEW ton.ops_hour_mv TO ton.ops_hour (`hour` DateTime('UTC'), `workchain` Int8, `op` Int64, `txs` UInt64, `failed` UInt64, `volume` UInt64, `fees` UInt64) AS SELECT toStartOfHour(now) AS hour, workchain, ifNull(toInt64(op), -1) AS op, toUInt64(count()) AS txs, toUInt64(countIf(success = 0)) AS failed, sum(value) AS volume, sum(total_fees) AS fees FROM ton.transactions WHERE tx_type NOT IN ('tick', 'tock') GROUP BY hour, workchain, op;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

DROP TABLE IF EXISTS ton.out_messages_buffer;

ALTER TABLE ton.out_messages
    ADD COLUMN IF NOT EXISTS created_lt UInt64,
    ADD COLUMN IF NOT EXISTS ihr_fee UInt64,
    ADD COLUMN IF NOT EXISTS extra_currencies Map(UInt32, UInt256);

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);

-- value flow of each block, nanoton: carried from the previous block and to
-- the next, imported/exported with messages, fees collected and burned; on
-- the masterchain also fees imported from shards, recovered, created
-- (validator reward) and minted.
ALTER TABLE ton.block_log
    ADD COLUMN IF NOT EXISTS from_prev UInt64,
    ADD COLUMN IF NOT EXISTS to_next UInt64,
    ADD COLUMN IF NOT EXISTS imported UInt64,
    ADD COLUMN IF NOT EXISTS exported UInt64,
    ADD COLUMN IF NOT EXISTS fees_collected UInt64,
    ADD COLUMN IF NOT EXISTS burned UInt64,
    ADD COLUMN IF NOT EXISTS fees_imported UInt64,
    ADD COLUMN IF NOT EXISTS recovered UInt64,
    ADD COLUMN IF NOT EXISTS created UInt64,
    ADD COLUMN IF NOT EXISTS minted UInt64;

-- which main validators signed mc blocks, per hour: every 10th mc block is
-- checked (sampled); signed/sampled = that validator's signing rate. idx is the
-- index in the set starting at utime_since (join ton.validators).
CREATE TABLE IF NOT EXISTS ton.validator_signatures (
    hour         DateTime('UTC'),
    utime_since  DateTime('UTC'),
    idx          UInt16,
    signed       UInt32,
    sampled      UInt32
)
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(hour)
ORDER BY (hour, utime_since, idx);

-- network config params as key blocks carry them: a row when a param's hash
-- changes (the first key block after the table appeared lists them all).
-- Hashes, not values: decode a param from the chain when needed.
CREATE TABLE IF NOT EXISTS ton.config_changes (
    mc_seqno  UInt32,
    now       DateTime('UTC'),
    param     Int32,
    hash      String,
    size      UInt32
)
ENGINE = ReplacingMergeTree
ORDER BY (param, mc_seqno);
