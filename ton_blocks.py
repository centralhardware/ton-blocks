#!/usr/bin/env python3
"""Log every new TON block's transactions to ClickHouse, for analytics.

No node of our own: the blocks are read from the public mainnet liteservers
(the global config pytoniq ships), parsed from BoC here, and stored. A block
*is* the diff, so nothing needs the chain state -- that is what the 64 GB
validator box would be for.

How one masterchain block is handled:

    mc block N        -> its own transactions (workchain -1)
    shard tops of N   -> each one, then its predecessors walked back via
                         prev_ref until we reach what the shard tops of N-1
                         already covered (several shard blocks can land
                         between two mc blocks, and shards split and merge)

Only what analytics needs is kept (the user's call, 2026-10-21): no hashes,
no per-block rows, no masterchain tick-tock housekeeping.

    ton.transactions   one slim row per transaction, TTL 90 days
    ton.minute         per-minute, per-workchain summary, kept forever --
                       filled by the ton.minute_mv view on every insert
                       (blocks, txs, failed, external_in, volume, fees,
                       out_msgs, unique accounts/senders, and accounts
                       created/deployed/frozen/deleted; read the uniq
                       columns with uniqMerge)
    ton.ops_hour       what kind of transactions: hourly counts by op code
                       (-1 = no body, 0 = text comment), kept forever --
                       hourly, because a single minute already sees ~350
                       distinct op codes, most of them a one-off long tail

Every row carries the `mc_seqno` it arrived with, and a restart resumes from
`max(mc_seqno) + 1`, so an outage backfills the gap instead of skipping it.
Each flush is a single INSERT, so the summary counts every batch once.

Running it (in the container the default command is the service):

    ton_blocks.py                      ensure the tables, then follow the chain
    ton_blocks.py --init               only create the tables
    ton_blocks.py --print-ddl          the CREATE statements
    ton_blocks.py --from 100400000     start/backfill from that mc seqno
    ton_blocks.py --dry-run --count 3  process 3 mc blocks, print, touch nothing

Configured by environment: CLICKHOUSE_URL, CLICKHOUSE_TON_USER,
CLICKHOUSE_TON_PASSWORD. The login needs SELECT, INSERT, CREATE TABLE and
CREATE VIEW on `ton.*`; the `ton` database itself is created by an admin.
"""
import argparse
import asyncio
import datetime
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

from pytoniq import LiteBalancer
from pytoniq_core.tl import BlockIdExt

DATABASE = "ton"
MC_SHARD = -2 ** 63
TIMEOUT = 60
FLUSH_SECONDS = 10
FLUSH_ROWS = 20000
POLL_SECONDS = 1.0
# touched after every mc block; the Docker HEALTHCHECK fails when it goes stale
HEARTBEAT = Path(os.environ.get("TON_HEARTBEAT_FILE", "/tmp/ton-blocks.heartbeat"))

TX_TTL_DAYS = 90

DDL = [
    # one slim row per transaction -- what analytics needs, none of the hashes,
    # block ids or masterchain tick-tock housekeeping; dropped after TX_TTL_DAYS
    """
CREATE TABLE IF NOT EXISTS {db}.transactions (
    now              DateTime('UTC'),
    mc_seqno         UInt32,
    workchain        Int8,
    block_seqno      UInt32,
    account          String CODEC(ZSTD(3)),
    lt               UInt64,
    in_msg_type      LowCardinality(String),
    src              String CODEC(ZSTD(3)),
    value            UInt64,
    op               Nullable(UInt32),
    total_fees       UInt64,
    success          UInt8,
    exit_code        Nullable(Int32),
    out_msgs         UInt16,
    orig_status      LowCardinality(String),
    end_status       LowCardinality(String)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(now)
ORDER BY (account, lt)
TTL now + INTERVAL {ttl} DAY
""",
    # the per-minute summary, kept forever; filled by the view below at insert
    """
CREATE TABLE IF NOT EXISTS {db}.minute (
    minute           DateTime('UTC'),
    workchain        Int8,
    blocks           AggregateFunction(uniq, UInt32),
    txs              SimpleAggregateFunction(sum, UInt64),
    failed           SimpleAggregateFunction(sum, UInt64),
    external_in      SimpleAggregateFunction(sum, UInt64),
    volume           SimpleAggregateFunction(sum, UInt64),
    fees             SimpleAggregateFunction(sum, UInt64),
    out_msgs         SimpleAggregateFunction(sum, UInt64),
    accounts         AggregateFunction(uniq, String),
    senders          AggregateFunction(uniq, String),
    created          SimpleAggregateFunction(sum, UInt64),
    deployed         SimpleAggregateFunction(sum, UInt64),
    frozen           SimpleAggregateFunction(sum, UInt64),
    deleted          SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(minute)
ORDER BY (workchain, minute)
""",
    """
CREATE MATERIALIZED VIEW IF NOT EXISTS {db}.minute_mv TO {db}.minute AS
SELECT
    toStartOfMinute(now)              AS minute,
    workchain,
    uniqState(block_seqno)            AS blocks,
    toUInt64(count())                 AS txs,
    toUInt64(countIf(success = 0))    AS failed,
    toUInt64(countIf(in_msg_type = 'externalin')) AS external_in,
    sum(value)                        AS volume,
    sum(total_fees)                   AS fees,
    toUInt64(sum(out_msgs))           AS out_msgs,
    uniqState(account)                AS accounts,
    uniqState(src)                    AS senders,
    -- account lifecycle, from the status before/after each transaction
    toUInt64(countIf(orig_status = 'nonexist' AND end_status != 'nonexist')) AS created,
    toUInt64(countIf(orig_status != 'active' AND end_status = 'active'))     AS deployed,
    toUInt64(countIf(orig_status != 'frozen' AND end_status = 'frozen'))     AS frozen,
    toUInt64(countIf(orig_status != 'nonexist' AND end_status = 'nonexist')) AS deleted
FROM {db}.transactions
GROUP BY minute, workchain
""",
    """
CREATE TABLE IF NOT EXISTS {db}.ops_hour (
    hour             DateTime('UTC'),
    workchain        Int8,
    op               Int64,
    txs              SimpleAggregateFunction(sum, UInt64),
    failed           SimpleAggregateFunction(sum, UInt64),
    volume           SimpleAggregateFunction(sum, UInt64),
    fees             SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(hour)
ORDER BY (workchain, op, hour)
""",
    """
CREATE MATERIALIZED VIEW IF NOT EXISTS {db}.ops_hour_mv TO {db}.ops_hour AS
SELECT
    toStartOfHour(now)                AS hour,
    workchain,
    ifNull(toInt64(op), -1)           AS op,
    toUInt64(count())                 AS txs,
    toUInt64(countIf(success = 0))    AS failed,
    sum(value)                        AS volume,
    sum(total_fees)                   AS fees
FROM {db}.transactions
GROUP BY hour, workchain, op
""",
]


# ------------------------------------------------------------------ clickhouse

def config(key, default=""):
    return os.environ.get(key) or default


def _post(query, body=None):
    url = config("CLICKHOUSE_URL")
    if not url:
        raise RuntimeError("CLICKHOUSE_URL not set")
    params = {
        "user": config("CLICKHOUSE_TON_USER"),
        "password": config("CLICKHOUSE_TON_PASSWORD"),
        "database": DATABASE,
    }
    if body is not None:
        params["query"] = query
    req = urllib.request.Request(
        url.rstrip("/") + "/?" + urllib.parse.urlencode(params),
        data=(body if body is not None else query).encode("utf-8"), method="POST")
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            return resp.read().decode()
    except urllib.error.HTTPError as e:
        raise RuntimeError("ClickHouse %s: %s" % (e.code, e.read().decode()[:600])) from None


def _insert(table, rows):
    if rows:
        body = "".join(json.dumps(r) + "\n" for r in rows)
        _post("INSERT INTO %s.%s FORMAT JSONEachRow" % (DATABASE, table), body=body)


def resume_point():
    """The mc seqno after the last one stored, or None on an empty table."""
    out = _post("SELECT max(mc_seqno), count() FROM %s.transactions FORMAT TSV" % DATABASE)
    top, n = out.split()
    return int(top) + 1 if int(n) else None


# ---------------------------------------------------------------------- chain

def _u64(shard):
    return shard & (2 ** 64 - 1)


def _i64(shard):
    return shard - 2 ** 64 if shard >= 2 ** 63 else shard


def _overlaps(a, b):
    """Two shard ids of one workchain cover intersecting address ranges."""
    a, b = _u64(a), _u64(b)
    la, lb = a & -a, b & -b
    lo = min(la, lb)
    # the coarser shard's prefix bits must match the finer one's
    big = la if la > lb else lb
    mask = ~((big << 1) - 1) & (2 ** 64 - 1)
    return lo and (a & mask) == (b & mask)


def _parent(shard):
    s = _u64(shard)
    low = s & -s
    return _i64((s - low) | (low << 1))


def _children(shard):
    s = _u64(shard)
    half = (s & -s) >> 1
    return _i64(s - half), _i64(s + half)


def _id(wc, shard, ref):
    return BlockIdExt(wc, shard, ref.seqno, ref.root_hash, ref.file_hash)


def prev_ids(blk_id, info):
    """The block ids this shard block was built on (one, or two after a merge)."""
    p = info.prev_ref
    if info.after_merge:
        left, right = _children(blk_id.shard)
        return [_id(blk_id.workchain, left, p.prev1), _id(blk_id.workchain, right, p.prev2)]
    shard = _parent(blk_id.shard) if info.after_split else blk_id.shard
    return [_id(blk_id.workchain, shard, p.prev)]


def _addr(a):
    if a is None:
        return ""
    try:
        return a.to_str(is_user_friendly=False)
    except Exception:
        return str(a)


def _op(msg):
    """The 32-bit op code at the head of the body (0 = text comment), or None."""
    try:
        body = msg.body.begin_parse()
        return body.load_uint(32) if body.remaining_bits >= 32 else None
    except Exception:
        return None


def _status(st):
    """AccountStatus as a plain word: active / uninitialized / frozen / nonexist."""
    return str(getattr(st, "type_", st)).lower()


def tx_row(blk_id, mc_seqno, tx):
    d = tx.description
    cph = getattr(d, "compute_ph", None)
    msg = tx.in_msg
    info = msg.info if msg is not None else None
    return {
        "now": tx.now,
        "mc_seqno": mc_seqno,
        "workchain": blk_id.workchain,
        "block_seqno": blk_id.seqno,
        "account": "%d:%s" % (blk_id.workchain, tx.account_addr_hex),
        "lt": tx.lt,
        # internal / externalin; empty for a transaction without an inbound message
        "in_msg_type": type(info).__name__.replace("MsgInfo", "").lower() if info is not None else "",
        "src": _addr(getattr(info, "src", None)),
        "value": getattr(info, "value_coins", 0) or 0,
        "op": _op(msg) if msg is not None else None,
        "total_fees": tx.total_fees.grams,
        "success": int(not getattr(d, "aborted", False)),
        "exit_code": getattr(cph, "exit_code", None) if cph else None,
        "out_msgs": tx.outmsg_cnt,
        "orig_status": _status(tx.orig_status),
        "end_status": _status(tx.end_status),
    }


class Follower:
    def __init__(self, dry_run=False):
        self.client = None
        self.dry_run = dry_run
        self.prev_tops = None      # shard tops of the last processed mc block
        self.txs = []
        self.last_flush = time.monotonic()

    async def connect(self):
        if self.client is not None:
            try:
                await self.client.close_all()
            except Exception:
                pass
        self.client = LiteBalancer.from_mainnet_config(trust_level=2, timeout=20)
        await self.client.start_up()

    async def mc_id(self, seqno):
        return (await self.client.lookup_block(-1, MC_SHARD, seqno))[0]

    async def one_block(self, blk_id, mc_seqno):
        """Buffer one block's transactions; returns its header info (for the prev walk)."""
        hdr = await self.client.raw_get_block_header(blk_id)
        txs = await self.client.raw_get_block_transactions_ext(blk_id)
        self.txs.extend(tx_row(blk_id, mc_seqno, t) for t in txs
                        if getattr(t.description, "type_", "") != "tick_tock")
        return hdr.info

    def _covered(self, blk_id, prev_tops):
        return any(t.workchain == blk_id.workchain and _overlaps(t.shard, blk_id.shard)
                   and t.seqno >= blk_id.seqno for t in prev_tops)

    async def process(self, seqno):
        mc = await self.mc_id(seqno)
        await self.one_block(mc, seqno)
        tops = await self.client.get_all_shards_info(mc)
        prev_tops = self.prev_tops
        if prev_tops is None:
            prev_tops = await self.client.get_all_shards_info(await self.mc_id(seqno - 1))
        seen = set()
        queue = list(tops)
        while queue:
            b = queue.pop()
            key = (b.workchain, b.shard, b.seqno)
            if key in seen or self._covered(b, prev_tops):
                continue
            seen.add(key)
            info = await self.one_block(b, seqno)
            queue.extend(prev_ids(b, info))
        self.prev_tops = tops
        return len(seen) + 1

    def flush(self, force=False):
        due = force or len(self.txs) >= FLUSH_ROWS or time.monotonic() - self.last_flush >= FLUSH_SECONDS
        if not due or not self.txs:
            return
        if self.dry_run:
            print("transactions: %d (first: %s)" % (len(self.txs), json.dumps(self.txs[0])), flush=True)
        else:
            # one INSERT per flush, so the minute view sees each batch exactly once
            _insert("transactions", self.txs)
        self.txs = []
        self.last_flush = time.monotonic()


async def run(args):
    f = Follower(dry_run=args.dry_run)
    await f.connect()
    nxt = args.start
    if nxt is None and not args.dry_run:
        nxt = resume_point()
    if nxt is None:
        nxt = f.client.last_mc_block.seqno
    print("following from mc seqno %d" % nxt, flush=True)
    done, failures = 0, 0
    while args.count is None or done < args.count:
        try:
            head = (await f.client.get_masterchain_info())["last"]["seqno"]
            if nxt > head:
                f.flush()
                await asyncio.sleep(POLL_SECONDS)
                continue
            n = await f.process(nxt)
            if args.dry_run or args.verbose:
                print("mc %d: %d blocks, lag %d" % (nxt, n, head - nxt), flush=True)
            nxt += 1
            done += 1
            HEARTBEAT.touch()
            failures = 0
            f.flush()
        except Exception as e:
            # a dropped liteserver or a ClickHouse hiccup: rows not yet flushed
            # stay buffered, the mc block is retried, reconnect with backoff
            failures += 1
            f.prev_tops = None
            wait = min(60, 2 ** failures)
            print("mc %d failed (%s: %s), retry in %ds" % (nxt, type(e).__name__, str(e)[:300], wait),
                  file=sys.stderr, flush=True)
            await asyncio.sleep(wait)
            if failures % 3 == 0:
                try:
                    await f.connect()
                except Exception as ce:
                    print("reconnect failed: %s" % ce, file=sys.stderr, flush=True)
    f.flush(force=True)
    await f.client.close_all()


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--print-ddl", action="store_true")
    ap.add_argument("--init", action="store_true")
    ap.add_argument("--from", dest="start", type=int)
    ap.add_argument("--count", type=int)
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args()
    if args.print_ddl:
        print("CREATE DATABASE IF NOT EXISTS %s;" % DATABASE)
        for s in DDL:
            print(s.format(db=DATABASE, ttl=TX_TTL_DAYS).strip() + ";\n")
        return
    if args.init or not args.dry_run:
        # idempotent (IF NOT EXISTS), so the service ensures its schema on every start
        for s in DDL:
            _post(s.format(db=DATABASE, ttl=TX_TTL_DAYS))
        print("tables ready", flush=True)
        if args.init:
            return
    asyncio.run(run(args))


if __name__ == "__main__":
    main()
