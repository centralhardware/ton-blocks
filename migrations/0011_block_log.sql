-- one row per block (masterchain and shard), for network and validator stats:
-- block times, shard splits/merges, which validator produced it. created_by is
-- the producing validator's public key (hex). Inserted in batches of ~30 s; a
-- block fetched twice (the gap check) collapses on merge.
CREATE TABLE IF NOT EXISTS ton.block_log (
    mc_seqno         UInt32,
    workchain        Int8,
    shard            String,
    seqno            UInt32,
    gen_utime        DateTime('UTC'),
    txs              UInt32,
    created_by       String CODEC(ZSTD(3)),
    key_block        UInt8,
    before_split     UInt8,
    after_split      UInt8,
    after_merge      UInt8,
    want_split       UInt8,
    want_merge       UInt8,
    catchain_seqno   UInt32,
    gen_version      UInt32
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(gen_utime)
ORDER BY (workchain, shard, seqno)
TTL gen_utime + INTERVAL 90 DAY;
