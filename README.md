# ton-blocks

Logs every new TON mainnet block's transactions into ClickHouse for analytics.
It reads blocks from the public liteservers through [pytoniq](https://github.com/yungwine/pytoniq), so you don't need to run your own node.

## What it stores (database `ton`)

| table | content | retention |
|---|---|---|
| `transactions` | one slim row per transaction: time, account, sender, value, op code, fees, success/exit code, account status before/after, and `trace_id`. No other hashes, no per-block rows, no masterchain tick-tock. | 90 days (TTL) |
| `minute` | per minute and workchain: blocks, txs, failed, external-in, volume, fees, out messages, unique accounts/senders (`uniqMerge`), accounts created/deployed/frozen/deleted | forever |
| `ops_hour` | per hour, workchain and op code (`-1` = no body, `0` = text comment): txs, failed, volume, fees | forever |
| `account_state` | every account seen in a transaction: latest status (argMax by lt), first/last seen, txs, failed, received, fees | forever |
| `accounts` (view) | one row per account over `account_state`, the current state | — |
| `account_status_counts` (view) | accounts per workchain and status | — |
| `op_names` | name, protocol, description and source for known op codes, loaded from `op_names.tsv` on every start | — |

The logger writes through a **Buffer** table, `transactions_buffer`. ClickHouse flushes it to `transactions` every 10–30 s, so `transactions` and the views behind it get about 2 parts a minute instead of one per insert. The buffer lives in ClickHouse's memory. A clean server shutdown flushes it, but a crash loses up to 30 s of rows. Every 5 minutes the logger checks the last ~25 minutes for masterchain blocks with no rows and fetches them again. If the server refuses the Buffer table, the logger inserts into `transactions` directly.

Materialized views fill `minute`, `ops_hour` and `account_state` on every insert into `transactions`. `account_state` covers only the accounts that have transacted since the logger started, not the whole chain. When it's first created, it's seeded from the transactions already stored.

On mainnet (October 2026) the logger sees about 2 masterchain blocks/s and about 35 tx/s, and it keeps up with no lag.

## Schema and migrations

The schema lives in `migrations/NNNN_name.sql`. On every start, the logger applies the files not yet recorded in `ton.schema_migrations`, in order, and records each one. To change the schema, add a new numbered file; never edit one that's already deployed. Statements in a file end with `;` at the end of a line. `python ton_blocks.py --print-ddl` prints them all.

A Buffer table can't be ALTERed. A migration that changes `transactions`' columns should `DROP TABLE ton.transactions_buffer` (dropping flushes it), `ALTER TABLE ton.transactions …`, then recreate the buffer as in `0005`.

`0001`–`0006` are the schema as it was when migrations were introduced. A database that already had that schema was marked at `0006` without running them.

## Traces

One user action (a swap, an NFT purchase) is one external message that sets off a tree of internal messages, so it spans several transactions, often in different blocks. `trace_id` is the hash of the transaction that started it, as a UInt256. Every transaction in the tree carries the same value. It is the id explorers use:

```sql
SELECT lower(hex(reverse(reinterpretAsFixedString(trace_id)))) AS trace_hex, count() AS steps
FROM ton.transactions WHERE trace_id != 0 GROUP BY trace_id ORDER BY steps DESC LIMIT 10
-- open as https://tonviewer.com/transaction/<trace_hex>
```

The logger links each incoming internal message to the trace of the transaction that sent it, keeping sent messages in memory for about 30 minutes. `0` means unknown: the sender was processed before the last restart (or is older than that), or the row predates the column (2026-10-21). The column costs about 32 bytes a row, roughly doubling the table, because hashes don't compress.


## How it follows the chain

For each masterchain block N, it stores the transactions in N itself. It then takes each shard top of N and walks back through `prev_ref` until it reaches what the shard tops of N-1 already covered. This handles several shard blocks landing between two masterchain blocks, and shard splits and merges.
After a restart it resumes from `max(mc_seqno) + 1`, so outages get backfilled.

## Deploy (Coolify)

1. As a ClickHouse admin:
   ```sql
   CREATE DATABASE ton;
   CREATE USER claude_ton IDENTIFIED BY '…';
   GRANT SELECT, INSERT, CREATE TABLE, CREATE VIEW, ALTER, DROP TABLE ON ton.* TO claude_ton;
   GRANT SELECT ON ton.* TO claude_ro;   -- for Grafana / read-only queries
   ```
2. In Coolify, create a new resource from **Docker Compose** using `docker-compose.yml` and set `CLICKHOUSE_TON_PASSWORD`. `CLICKHOUSE_URL` and `CLICKHOUSE_TON_USER` have defaults. The service applies its migrations on start.
3. Optional: add the Coolify deploy webhook as `COOLIFY_DEPLOY_HOOK_URL` / `COOLIFY_DEPLOY_BEARER_TOKEN` repo secrets. Each push to `main` then builds `ghcr.io/centralhardware/ton-blocks` and redeploys.

The container reports unhealthy if it hasn't processed a masterchain block for 3 minutes.

## Local

```sh
pip install -r requirements.txt
python ton_blocks.py --dry-run --count 3     # parse live blocks, write nothing
python ton_blocks.py --print-ddl
```
