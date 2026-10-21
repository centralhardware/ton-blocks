-- The logger inserts here. ClickHouse flushes it into transactions after 10-30 s
-- (or 500k rows / 200 MB), so transactions and every view behind it get ~2 parts
-- a minute instead of one per insert. It lives in server memory: a crash loses
-- up to 30 s of rows, which the logger's gap check fetches again.
-- A Buffer table can't be ALTERed: to change transactions' columns, DROP this
-- (that flushes it), ALTER transactions, then create it again.
CREATE TABLE IF NOT EXISTS ton.transactions_buffer AS ton.transactions
ENGINE = Buffer(ton, transactions, 1, 10, 30, 10000, 500000, 10000000, 200000000);
