-- smaller columns everywhere: Delta+ZSTD on the numeric sort-key columns and
-- on transactions'/out_messages' times and seqnos (they grow in small steps in
-- sort order, account then lt), ZSTD(3) instead of the default
-- LZ4 on every other column that had no codec. Hashes (UInt256), Bool,
-- LowCardinality, Map and Array columns keep their defaults. Codec-only
-- MODIFY COLUMN: types and data stay; OPTIMIZE ... FINAL then rewrites the
-- existing parts with the new codecs (it also merges duplicates, which
-- ReplacingMergeTree does anyway).
-- Buffers can't ALTER and sit on top of two of these: dropped and recreated.
DROP TABLE IF EXISTS ton.transactions_buffer;
DROP TABLE IF EXISTS ton.out_messages_buffer;

ALTER TABLE ton.account_state
    MODIFY COLUMN workchain CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN status CODEC(ZSTD(3)),
    MODIFY COLUMN first_seen CODEC(ZSTD(3)),
    MODIFY COLUMN last_seen CODEC(ZSTD(3)),
    MODIFY COLUMN txs CODEC(ZSTD(3)),
    MODIFY COLUMN failed CODEC(ZSTD(3)),
    MODIFY COLUMN received CODEC(ZSTD(3)),
    MODIFY COLUMN fees CODEC(ZSTD(3));

ALTER TABLE ton.balances
    MODIFY COLUMN balance CODEC(ZSTD(3)),
    MODIFY COLUMN at CODEC(ZSTD(3));

ALTER TABLE ton.block_log
    MODIFY COLUMN mc_seqno CODEC(ZSTD(3)),
    MODIFY COLUMN workchain CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN shard CODEC(ZSTD(3)),
    MODIFY COLUMN seqno CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN gen_utime CODEC(ZSTD(3)),
    MODIFY COLUMN txs CODEC(ZSTD(3)),
    MODIFY COLUMN key_block CODEC(ZSTD(3)),
    MODIFY COLUMN before_split CODEC(ZSTD(3)),
    MODIFY COLUMN after_split CODEC(ZSTD(3)),
    MODIFY COLUMN after_merge CODEC(ZSTD(3)),
    MODIFY COLUMN want_split CODEC(ZSTD(3)),
    MODIFY COLUMN want_merge CODEC(ZSTD(3)),
    MODIFY COLUMN catchain_seqno CODEC(ZSTD(3)),
    MODIFY COLUMN gen_version CODEC(ZSTD(3)),
    MODIFY COLUMN from_prev CODEC(ZSTD(3)),
    MODIFY COLUMN to_next CODEC(ZSTD(3)),
    MODIFY COLUMN imported CODEC(ZSTD(3)),
    MODIFY COLUMN exported CODEC(ZSTD(3)),
    MODIFY COLUMN fees_collected CODEC(ZSTD(3)),
    MODIFY COLUMN burned CODEC(ZSTD(3)),
    MODIFY COLUMN fees_imported CODEC(ZSTD(3)),
    MODIFY COLUMN recovered CODEC(ZSTD(3)),
    MODIFY COLUMN created CODEC(ZSTD(3)),
    MODIFY COLUMN minted CODEC(ZSTD(3));

ALTER TABLE ton.code
    MODIFY COLUMN code_hash CODEC(ZSTD(3)),
    MODIFY COLUMN size CODEC(ZSTD(3)),
    MODIFY COLUMN example CODEC(ZSTD(3)),
    MODIFY COLUMN resolved_at CODEC(ZSTD(3)),
    MODIFY COLUMN asm_error CODEC(ZSTD(3));

ALTER TABLE ton.code_sources
    MODIFY COLUMN code_hash CODEC(ZSTD(3)),
    MODIFY COLUMN compiler_version CODEC(ZSTD(3)),
    MODIFY COLUMN command_line CODEC(ZSTD(3)),
    MODIFY COLUMN ipfs CODEC(ZSTD(3)),
    MODIFY COLUMN checked_at CODEC(ZSTD(3));

ALTER TABLE ton.config_changes
    MODIFY COLUMN mc_seqno CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN now CODEC(ZSTD(3)),
    MODIFY COLUMN param CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN hash CODEC(ZSTD(3)),
    MODIFY COLUMN size CODEC(ZSTD(3));

ALTER TABLE ton.contracts
    MODIFY COLUMN resolved_at CODEC(ZSTD(3));

ALTER TABLE ton.jetton_wallets
    MODIFY COLUMN resolved_at CODEC(ZSTD(3));

ALTER TABLE ton.jettons
    MODIFY COLUMN jetton CODEC(ZSTD(3)),
    MODIFY COLUMN symbol CODEC(ZSTD(3)),
    MODIFY COLUMN name CODEC(ZSTD(3)),
    MODIFY COLUMN decimals CODEC(ZSTD(3)),
    MODIFY COLUMN image CODEC(ZSTD(3)),
    MODIFY COLUMN resolved_at CODEC(ZSTD(3));

ALTER TABLE ton.minute
    MODIFY COLUMN minute CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN workchain CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN blocks CODEC(ZSTD(3)),
    MODIFY COLUMN txs CODEC(ZSTD(3)),
    MODIFY COLUMN failed CODEC(ZSTD(3)),
    MODIFY COLUMN external_in CODEC(ZSTD(3)),
    MODIFY COLUMN volume CODEC(ZSTD(3)),
    MODIFY COLUMN fees CODEC(ZSTD(3)),
    MODIFY COLUMN out_msgs CODEC(ZSTD(3)),
    MODIFY COLUMN accounts CODEC(ZSTD(3)),
    MODIFY COLUMN senders CODEC(ZSTD(3)),
    MODIFY COLUMN created CODEC(ZSTD(3)),
    MODIFY COLUMN deployed CODEC(ZSTD(3)),
    MODIFY COLUMN frozen CODEC(ZSTD(3)),
    MODIFY COLUMN deleted CODEC(ZSTD(3));

ALTER TABLE ton.nft_collections
    MODIFY COLUMN name CODEC(ZSTD(3));

ALTER TABLE ton.nft_items
    MODIFY COLUMN idx CODEC(ZSTD(3)),
    MODIFY COLUMN name CODEC(ZSTD(3)),
    MODIFY COLUMN at CODEC(ZSTD(3));

ALTER TABLE ton.ops_hour
    MODIFY COLUMN hour CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN workchain CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN op CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN txs CODEC(ZSTD(3)),
    MODIFY COLUMN failed CODEC(ZSTD(3)),
    MODIFY COLUMN volume CODEC(ZSTD(3)),
    MODIFY COLUMN fees CODEC(ZSTD(3));

ALTER TABLE ton.out_messages
    MODIFY COLUMN now CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN mc_seqno CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN lt CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN idx CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN value CODEC(ZSTD(3)),
    MODIFY COLUMN op CODEC(ZSTD(3)),
    MODIFY COLUMN bounce CODEC(ZSTD(3)),
    MODIFY COLUMN bounced CODEC(ZSTD(3)),
    MODIFY COLUMN fwd_fee CODEC(ZSTD(3)),
    MODIFY COLUMN created_lt CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN ihr_fee CODEC(ZSTD(3));

ALTER TABLE ton.swaps
    MODIFY COLUMN now CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN mc_seqno CODEC(ZSTD(3)),
    MODIFY COLUMN amount_in CODEC(ZSTD(3)),
    MODIFY COLUMN amount_out CODEC(ZSTD(3));

ALTER TABLE ton.transactions
    MODIFY COLUMN now CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN mc_seqno CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN workchain CODEC(ZSTD(3)),
    MODIFY COLUMN block_seqno CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN lt CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN value CODEC(ZSTD(3)),
    MODIFY COLUMN op CODEC(ZSTD(3)),
    MODIFY COLUMN jetton_amount CODEC(ZSTD(3)),
    MODIFY COLUMN total_fees CODEC(ZSTD(3)),
    MODIFY COLUMN storage_fees CODEC(ZSTD(3)),
    MODIFY COLUMN gas_fees CODEC(ZSTD(3)),
    MODIFY COLUMN fwd_fees CODEC(ZSTD(3)),
    MODIFY COLUMN action_fees CODEC(ZSTD(3)),
    MODIFY COLUMN gas_used CODEC(ZSTD(3)),
    MODIFY COLUMN vm_steps CODEC(ZSTD(3)),
    MODIFY COLUMN bounce CODEC(ZSTD(3)),
    MODIFY COLUMN bounced CODEC(ZSTD(3)),
    MODIFY COLUMN success CODEC(ZSTD(3)),
    MODIFY COLUMN exit_code CODEC(ZSTD(3)),
    MODIFY COLUMN action_code CODEC(ZSTD(3)),
    MODIFY COLUMN out_msgs CODEC(ZSTD(3)),
    MODIFY COLUMN destroyed CODEC(ZSTD(3)),
    MODIFY COLUMN gas_limit CODEC(ZSTD(3)),
    MODIFY COLUMN gas_credit CODEC(ZSTD(3)),
    MODIFY COLUMN exit_arg CODEC(ZSTD(3)),
    MODIFY COLUMN total_actions CODEC(ZSTD(3)),
    MODIFY COLUMN skipped_actions CODEC(ZSTD(3)),
    MODIFY COLUMN msgs_created CODEC(ZSTD(3)),
    MODIFY COLUMN msg_cells CODEC(ZSTD(3)),
    MODIFY COLUMN msg_bits CODEC(ZSTD(3)),
    MODIFY COLUMN ihr_fee CODEC(ZSTD(3)),
    MODIFY COLUMN in_created_lt CODEC(Delta, ZSTD(3));

ALTER TABLE ton.validator_signatures
    MODIFY COLUMN hour CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN utime_since CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN idx CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN signed CODEC(ZSTD(3)),
    MODIFY COLUMN sampled CODEC(ZSTD(3));

ALTER TABLE ton.validators
    MODIFY COLUMN param CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN utime_since CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN utime_until CODEC(ZSTD(3)),
    MODIFY COLUMN idx CODEC(Delta, ZSTD(3)),
    MODIFY COLUMN pubkey CODEC(ZSTD(3)),
    MODIFY COLUMN weight CODEC(ZSTD(3)),
    MODIFY COLUMN share CODEC(ZSTD(3)),
    MODIFY COLUMN adnl CODEC(ZSTD(3));

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);

CREATE TABLE IF NOT EXISTS ton.out_messages_buffer AS ton.out_messages
ENGINE = Buffer(ton, out_messages, 1, 10, 30, 10000, 500000, 10000000, 200000000);

OPTIMIZE TABLE ton.account_state FINAL;
OPTIMIZE TABLE ton.balances FINAL;
OPTIMIZE TABLE ton.block_log FINAL;
OPTIMIZE TABLE ton.code FINAL;
OPTIMIZE TABLE ton.code_sources FINAL;
OPTIMIZE TABLE ton.config_changes FINAL;
OPTIMIZE TABLE ton.contracts FINAL;
OPTIMIZE TABLE ton.jetton_wallets FINAL;
OPTIMIZE TABLE ton.jettons FINAL;
OPTIMIZE TABLE ton.minute FINAL;
OPTIMIZE TABLE ton.nft_collections FINAL;
OPTIMIZE TABLE ton.nft_items FINAL;
OPTIMIZE TABLE ton.ops_hour FINAL;
OPTIMIZE TABLE ton.out_messages FINAL;
OPTIMIZE TABLE ton.swaps FINAL;
OPTIMIZE TABLE ton.transactions FINAL;
OPTIMIZE TABLE ton.validator_signatures FINAL;
OPTIMIZE TABLE ton.validators FINAL;
