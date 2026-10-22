-- verified source code of contract code (verifier.ton.org / orbs.com): the
-- original FunC/Tact/Tolk/Fift files the author published, found through the
-- on-chain sources registry by code hash and downloaded from IPFS. One row per
-- code hash; verified = 0 means no verifier holds sources for it (asked again
-- after a week). Join ton.code / ton.contracts on code_hash.
CREATE TABLE IF NOT EXISTS ton.code_sources (
    code_hash         String,
    verified          Bool,
    verifier          LowCardinality(String),
    compiler          LowCardinality(String),
    compiler_version  String,
    command_line      String,
    files             Nested(name String, content String CODEC(ZSTD(9)), entrypoint Bool),
    ipfs              String,
    checked_at        DateTime('UTC')
)
ENGINE = ReplacingMergeTree(checked_at)
ORDER BY code_hash;
