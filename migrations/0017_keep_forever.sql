-- keep everything forever; old partitions are to be moved to cold storage
-- instead of expiring. REMOVE TTL changes metadata only.
ALTER TABLE ton.transactions REMOVE TTL;
ALTER TABLE ton.block_log REMOVE TTL;
ALTER TABLE ton.swaps REMOVE TTL;
