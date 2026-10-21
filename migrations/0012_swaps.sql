-- DEX swaps (DeDust, STON.fi v1/v2), decoded from the pool's transaction; see
-- swaps.go. Amounts in the tokens' smallest units. DeDust names tokens by jetton
-- master ('' = native TON), STON.fi by the router's jetton wallet: resolve both
-- with coalesce(jetton_wallets.jetton, token).
CREATE TABLE IF NOT EXISTS ton.swaps (
    now              DateTime('UTC'),
    mc_seqno         UInt32,
    trace_id         UInt256,
    dex              LowCardinality(String),
    pool             String CODEC(ZSTD(3)),
    user             String CODEC(ZSTD(3)),
    token_in         String CODEC(ZSTD(3)),
    amount_in        UInt128,
    token_out        String CODEC(ZSTD(3)),
    amount_out       UInt128
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(now)
ORDER BY (pool, now, trace_id, token_in, amount_in)
TTL now + INTERVAL 90 DAY;
