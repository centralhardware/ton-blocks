-- the token amount of jetton messages, parsed from the body, in the token's
-- smallest units (divide by 10^jettons.decimals):
--   transfer 0x0f8a7ea5        (to the sender's wallet)    peer = destination owner
--   internal_transfer 0x178d4519 (to the receiver's wallet) peer = sending owner
--   transfer_notification 0x7362d09c (to the receiver)     peer = sending owner
--   burn 0x595f07bc            (to the burner's wallet)    peer = ''
-- NULL for every other message. Count volume on internal_transfer: one per
-- transfer, and its account is the receiving jetton wallet (join jetton_wallets).
DROP TABLE IF EXISTS ton.transactions_buffer;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS jetton_amount Nullable(UInt128) AFTER op,
    ADD COLUMN IF NOT EXISTS jetton_peer String CODEC(ZSTD(3)) AFTER jetton_amount;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
