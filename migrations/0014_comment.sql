-- text comments: the body of op 0 messages, and the forward payload of jetton
-- transfer_notification (0x7362d09c), which carries a token transfer's comment.
-- Valid UTF-8 only, cut at 512 bytes; '' for everything else.
DROP TABLE IF EXISTS ton.transactions_buffer;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS comment String CODEC(ZSTD(3)) AFTER jetton_peer;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
