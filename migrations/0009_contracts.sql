-- the code each account runs, resolved through toncenter in the background (see
-- contracts.go): code_hash is the base64 hash of the code cell, interfaces what
-- toncenter recognises it as (wallet_v5r1, jetton_wallet, nft_item, ...; empty =
-- unknown code). code_hash = '' means no code (not deployed) or toncenter didn't
-- answer for it; such accounts are asked again after a few hours.
CREATE TABLE IF NOT EXISTS ton.contracts (
    account          String CODEC(ZSTD(3)),
    status           LowCardinality(String),
    code_hash        String CODEC(ZSTD(3)),
    interfaces       Array(LowCardinality(String)),
    resolved_at      DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(resolved_at)
ORDER BY account;
