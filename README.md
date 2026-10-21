# ton-blocks

Logs every new TON mainnet block's transactions into ClickHouse for analytics.
It reads blocks from the public liteservers through [pytoniq](https://github.com/yungwine/pytoniq), so you don't need to run your own node.

## What it stores (database `ton`)

| table | content | retention |
|---|---|---|
| `transactions` | one slim row per transaction: time, account, sender, value, op code, fees, success/exit code, account status before/after. No hashes, no per-block rows, no masterchain tick-tock. | 90 days (TTL) |
| `minute` | per minute and workchain: blocks, txs, failed, external-in, volume, fees, out messages, unique accounts/senders (`uniqMerge`), accounts created/deployed/frozen/deleted | forever |
| `ops_hour` | per hour, workchain and op code (`-1` = no body, `0` = text comment): txs, failed, volume, fees | forever |

A materialized view fills `minute` and `ops_hour` on every insert into `transactions`.

On mainnet (October 2026) the logger sees about 2 masterchain blocks/s and about 35 tx/s, and it keeps up with no lag.

## How it follows the chain

For each masterchain block N, it stores the transactions in N itself. It then takes each shard top of N and walks back through `prev_ref` until it reaches what the shard tops of N-1 already covered. This handles several shard blocks landing between two masterchain blocks, and shard splits and merges.
After a restart it resumes from `max(mc_seqno) + 1`, so outages get backfilled.

## Deploy (Coolify)

1. As a ClickHouse admin:
   ```sql
   CREATE DATABASE ton;
   CREATE USER claude_ton IDENTIFIED BY '…';
   GRANT SELECT, INSERT, CREATE TABLE, CREATE VIEW ON ton.* TO claude_ton;
   GRANT SELECT ON ton.* TO claude_ro;   -- for Grafana / read-only queries
   ```
2. In Coolify, create a new resource from **Docker Compose** using `docker-compose.yml` and set `CLICKHOUSE_TON_PASSWORD`. `CLICKHOUSE_URL` and `CLICKHOUSE_TON_USER` have defaults. The service creates its tables on start.
3. Optional: add the Coolify deploy webhook as `COOLIFY_DEPLOY_HOOK_URL` / `COOLIFY_DEPLOY_BEARER_TOKEN` repo secrets. Each push to `main` then builds `ghcr.io/centralhardware/ton-blocks` and redeploys.

The container reports unhealthy if it hasn't processed a masterchain block for 3 minutes.

## Local

```sh
pip install -r requirements.txt
python ton_blocks.py --dry-run --count 3     # parse live blocks, write nothing
python ton_blocks.py --print-ddl
```
