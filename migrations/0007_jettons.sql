-- which token each jetton wallet belongs to, and the tokens' names. Blocks only
-- carry the wallet address, so the logger resolves wallets it sees through
-- toncenter in the background (see resolveJettons). jetton = '' means toncenter
-- didn't know the wallet; it is asked again after a few hours.
CREATE TABLE IF NOT EXISTS ton.jetton_wallets (
    wallet           String CODEC(ZSTD(3)),
    jetton           String CODEC(ZSTD(3)),
    owner            String CODEC(ZSTD(3)),
    resolved_at      DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(resolved_at)
ORDER BY wallet;

CREATE TABLE IF NOT EXISTS ton.jettons (
    jetton           String,
    symbol           String,
    name             String,
    decimals         UInt8,
    image            String,
    resolved_at      DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(resolved_at)
ORDER BY jetton;
