-- why a transaction failed when the compute phase gives no exit code:
--   skip_reason  the compute phase didn't run: NO_STATE (account not deployed
--                and the message has no init), BAD_STATE, NO_GAS, SUSPENDED
--   action_code  the action phase's result code (sending messages, reserving):
--                0 = fine, e.g. 37 = not enough TON, 34 = bad action
-- The Buffer table can't be ALTERed: drop it (that flushes it), alter, recreate.
DROP TABLE IF EXISTS ton.transactions_buffer;

ALTER TABLE ton.transactions
    ADD COLUMN IF NOT EXISTS skip_reason LowCardinality(String) DEFAULT '' AFTER exit_code,
    ADD COLUMN IF NOT EXISTS action_code Nullable(Int32) AFTER skip_reason;

CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
