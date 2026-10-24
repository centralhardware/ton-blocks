-- TON DNS record changes (change_dns_record to a .ton / t.me domain item), see
-- dns.go. name = the standard record (wallet, site, storage, dns_next_resolver),
-- '' for other keys; kind delete = the record was removed. The domain itself:
-- join nft_items on item.
CREATE TABLE IF NOT EXISTS ton.dns_changes (
    now              DateTime('UTC') CODEC(Delta, ZSTD(3)),
    mc_seqno         UInt32 CODEC(ZSTD(3)),
    trace_id         UInt256,
    item             String CODEC(ZSTD(3)),
    sender           String CODEC(ZSTD(3)),
    key              String CODEC(ZSTD(3)),
    name             LowCardinality(String),
    kind             LowCardinality(String),
    value            String CODEC(ZSTD(3)),
    success          UInt8 CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(now)
ORDER BY (item, now, trace_id, key);
