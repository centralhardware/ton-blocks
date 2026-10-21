# ton-blocks

Logs every new TON mainnet block's transactions into ClickHouse for analytics.
It's written in Go and reads blocks from the public liteservers through [tonutils-go](https://github.com/xssnick/tonutils-go), so you don't need to run your own node. The migrations and op name files are embedded in the binary.

## What it stores (database `ton`)

| table | content | retention |
|---|---|---|
| `transactions` | one slim row per transaction: time, account, sender, value, op code, fees, token amount and counterparty of jetton messages (`jetton_amount`, `jetton_peer`), success/exit code, why the code didn't run (`skip_reason`), action phase result (`action_code`), account status before/after, and `trace_id`. No other hashes, no per-block columns (those are in `block_log`), no masterchain tick-tock. | 90 days (TTL) |
| `minute` | per minute and workchain: blocks, txs, failed, external-in, volume, fees, out messages, unique accounts/senders (`uniqMerge`), accounts created/deployed/frozen/deleted | forever |
| `ops_hour` | per hour, workchain and op code (`-1` = no body, `0` = text comment): txs, failed, volume, fees | forever |
| `account_state` | every account seen in a transaction: latest status (argMax by lt), first/last seen, txs, failed, received, fees | forever |
| `accounts` (view) | one row per account over `account_state`, the current state | — |
| `account_status_counts` (view) | accounts per workchain and status | — |
| `jetton_wallets` | token wallet → its token (`jetton` master) and owner, resolved through toncenter; `''` = unknown | — |
| `jettons` | token master → symbol, name, decimals, image | — |
| `contracts` | account → status, code hash and the interfaces toncenter recognises in that code (`wallet_v5r1`, `jetton_wallet`, `tg_wallet`, …) | — |
| `code` | the code itself, once per distinct code hash: base64 BOC (bytecode), size, an example account | — |
| `block_log` | one row per masterchain and shard block: time, transactions, producing validator (`created_by`), key block, split/merge flags, catchain seqno, node version | 90 days (TTL) |
| `swaps` | DEX swaps (DeDust, STON.fi v1/v2): pool, user, token and amount in, token and amount out, trace | 90 days (TTL) |
| `op_names` | name, protocol, description and source for known op codes, loaded from `op_names.tsv` on every start | — |

The logger writes through a **Buffer** table, `transactions_buffer`. ClickHouse flushes it to `transactions` every 10–30 s, so `transactions` and the views behind it get about 2 parts a minute instead of one per insert. The buffer lives in ClickHouse's memory. A clean server shutdown flushes it, but a crash loses up to 30 s of rows. Every 5 minutes the logger checks the last ~25 minutes for masterchain blocks with no rows and fetches them again. If the server refuses the Buffer table, the logger inserts into `transactions` directly.

Materialized views fill `minute`, `ops_hour` and `account_state` on every insert into `transactions`. `account_state` covers only the accounts that have transacted since the logger started, not the whole chain. When it's first created, it's seeded from the transactions already stored.

On mainnet (October 2026) the logger sees about 2 masterchain blocks/s and about 35 tx/s, and it keeps up with no lag. Catching up after an outage, it processes about 12 masterchain blocks/s (measured: 100 blocks in 8.5 s).

## Schema and migrations

The schema lives in `migrations/NNNN_name.sql`. On every start, the logger applies the files not yet recorded in `ton.schema_migrations`, in order, and records each one. To change the schema, add a new numbered file; never edit one that's already deployed. Statements in a file end with `;` at the end of a line. `go run . -print-ddl` prints them all.

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

## Tokens

A jetton transfer only names the token *wallet* involved, not the token. Which token a wallet belongs to is chain state, so a background loop in the logger asks toncenter's index (`/api/v3/jetton/wallets`, 100 addresses a request, 1 request/s without a key, or set `TONCENTER_API_KEY`) about every wallet that received a transfer, internal_transfer or burn in the last 7 days and isn't resolved yet. Wallets toncenter doesn't know are asked again after 6 hours. The follower never waits on it.

```sql
SELECT j.symbol, count() AS transfers
FROM ton.transactions t
JOIN ton.jetton_wallets w FINAL ON w.wallet = t.account
JOIN ton.jettons j FINAL ON j.jetton = w.jetton
WHERE t.op = 0x178d4519 AND t.now > now() - 86400   -- internal_transfer: one per transfer
GROUP BY j.symbol ORDER BY transfers DESC LIMIT 20
```
## Contract types

Which code an account runs is chain state too. A second background loop asks toncenter (`/api/v3/accountStates`, 100 accounts a request) about every account that transacted in the last 7 days and stores its code hash and interfaces in `contracts`. Accounts without code (not deployed) are asked again after 6 hours; an account is looked up once, so a later code upgrade isn't noticed. After each round it also fetches the code of hashes it hasn't seen before (one example account per hash, 20 per request) into `code`. That's bytecode; source exists only where the author verified it (verifier.ton.org). Both loops share one toncenter rate limiter (about 1 request/s, retrying on 429).

```sql
SELECT if(empty(c.interfaces), 'unknown code', c.interfaces[1]) AS type, count() AS txs, countIf(t.success = 0) AS failed
FROM ton.transactions t JOIN ton.contracts c FINAL ON c.account = t.account
WHERE t.now > now() - 3600 GROUP BY type ORDER BY txs DESC
```
## Swaps

`swaps.go` decodes swaps from the pool's own transaction: DeDust pools emit an external SwapEvent with both jetton masters and amounts; STON.fi v1/v2 pools receive a `swap` with the input amount and send `pay_to` with the output. STON.fi names tokens by the router's jetton wallet, so resolve a token with `coalesce(nullIf(jetton_wallets.jetton, ''), token)`; `''` is native TON (DeDust). Refunds are not swaps and are skipped.

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
go run . -dry-run -count 3     # parse live blocks, write nothing
go run . -print-ddl
go run . -from 100400000       # backfill from that mc seqno
```
