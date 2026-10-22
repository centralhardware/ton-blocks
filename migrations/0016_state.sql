-- chain state the blocks don't carry; see state.go.
-- balances: nanoton, re-read from toncenter after an account transacts (at most
-- every 5 min). Latest = argMax(balance, at).
CREATE TABLE IF NOT EXISTS ton.balances (
    account  String CODEC(ZSTD(3)),
    balance  UInt64,
    at       DateTime('UTC')
)
ENGINE = ReplacingMergeTree(at)
ORDER BY account;

-- NFT items, re-read after each NFT transfer to them; owner is the real owner
-- (not the marketplace contract while on sale).
CREATE TABLE IF NOT EXISTS ton.nft_items (
    item        String CODEC(ZSTD(3)),
    collection  String CODEC(ZSTD(3)),
    owner       String CODEC(ZSTD(3)),
    idx         String,
    name        String,
    image       String CODEC(ZSTD(3)),
    on_sale     Bool,
    at          DateTime('UTC')
)
ENGINE = ReplacingMergeTree(at)
ORDER BY item;

CREATE TABLE IF NOT EXISTS ton.nft_collections (
    collection  String CODEC(ZSTD(3)),
    name        String,
    image       String CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree
ORDER BY collection;

-- validator sets from config param 34 (current) and 36 (next, only between an
-- election and the switch). pubkey matches block_log.created_by. weight is
-- proportional to stake; share = weight / total of the set.
CREATE TABLE IF NOT EXISTS ton.validators (
    param        UInt8,
    utime_since  DateTime('UTC'),
    utime_until  DateTime('UTC'),
    idx          UInt16,
    pubkey       String,
    weight       UInt64,
    share        Float64,
    adnl         String,
    main         Bool
)
ENGINE = ReplacingMergeTree
ORDER BY (utime_since, param, idx);
