-- per-phase fees, gas, bounce flags and deployed code of each transaction:
--   storage_fees / gas_fees / fwd_fees (sent messages) / action_fees, in nanoton
--   gas_used, vm_steps: compute phase (0 when it was skipped)
--   bounce: the incoming message asked to bounce on failure; bounced: it IS a bounce
--   init_code_hash: code hash of the StateInit the incoming message carried
--   (a deployment), base64 like ton.code.code_hash; '' for none
DROP TABLE IF EXISTS ton.transactions_buffer;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS storage_fees UInt64 AFTER total_fees,
    ADD COLUMN IF NOT EXISTS gas_fees UInt64 AFTER storage_fees,
    ADD COLUMN IF NOT EXISTS fwd_fees UInt64 AFTER gas_fees,
    ADD COLUMN IF NOT EXISTS action_fees UInt64 AFTER fwd_fees,
    ADD COLUMN IF NOT EXISTS gas_used UInt64 AFTER action_fees,
    ADD COLUMN IF NOT EXISTS vm_steps UInt32 AFTER gas_used,
    ADD COLUMN IF NOT EXISTS bounce UInt8 AFTER vm_steps,
    ADD COLUMN IF NOT EXISTS bounced UInt8 AFTER bounce,
    ADD COLUMN IF NOT EXISTS init_code_hash String CODEC(ZSTD(3)) AFTER bounced;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

-- every message a transaction sent (account = sender, lt = its transaction).
-- type 'externalout' = a contract event/log: dest is the external address in
-- hex ('' if none), event_boc its body (base64 BOC, '' over 16 KiB).
CREATE TABLE IF NOT EXISTS ton.out_messages (
    now        DateTime('UTC'),
    mc_seqno   UInt32,
    trace_id   UInt256,
    account    String CODEC(ZSTD(3)),
    lt         UInt64,
    idx        UInt16,
    type       LowCardinality(String),
    dest       String CODEC(ZSTD(3)),
    value      UInt64,
    op         Nullable(UInt32),
    bounce     UInt8,
    bounced    UInt8,
    fwd_fee    UInt64,
    comment    String CODEC(ZSTD(3)),
    event_boc  String CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(now)
ORDER BY (account, lt, idx)
TTL now + INTERVAL 90 DAY;

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);

-- the raw body of each transaction's incoming message (base64 BOC; bodies over
-- 16 KiB are skipped). Large and incompressible, so kept 14 days only.
CREATE TABLE IF NOT EXISTS ton.message_bodies (
    now      DateTime('UTC'),
    account  String CODEC(ZSTD(3)),
    lt       UInt64,
    boc      String CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(now)
ORDER BY (account, lt)
TTL now + INTERVAL 14 DAY;

CREATE TABLE IF NOT EXISTS ton.message_bodies_buffer AS ton.message_bodies
ENGINE = Buffer(ton, message_bodies, 1, 10, 30, 10000, 500000, 10000000, 200000000);
