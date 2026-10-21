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
no per-block rows, no masterchain tick-tock housekeeping. The one exception is
`trace_id`: the hash of the transaction that started the trace (the external
message a user signed), shared by every transaction that message set off, so
one user action -- a swap, an NFT purchase -- can be grouped. It is the id the
explorers use: tonviewer.com/transaction/<trace_hex(trace_id)>. 0 = unknown
(the parent was seen before the last restart, or is older than TRACE_KEEP).

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
    ton.account_state  every account seen, latest status (argMax by lt),
                       first/last seen, txs, received, fees -- forever
    ton.accounts       view: one row per account over account_state
    ton.account_status_counts  view: accounts per workchain and status

Every row carries the `mc_seqno` it arrived with, and a restart resumes from
`max(mc_seqno) + 1`, so an outage backfills the gap instead of skipping it.
Each flush is a single INSERT, so the summary counts every batch once.

Running it (in the container the default command is the service):

    ton_blocks.py                      ensure the tables, then follow the chain
    ton_blocks.py --init               only apply the migrations
    ton_blocks.py --print-ddl          the migrations' statements
    ton_blocks.py --from 100400000     start/backfill from that mc seqno
    ton_blocks.py --dry-run --count 3  process 3 mc blocks, print, touch nothing

Configured by environment: CLICKHOUSE_URL, CLICKHOUSE_TON_USER,
CLICKHOUSE_TON_PASSWORD. The login needs SELECT, INSERT, CREATE TABLE, CREATE
VIEW, ALTER and DROP TABLE on `ton.*` (the last two for migrations that change
a table); the `ton` database itself is created by an admin.
"""
import argparse
import asyncio
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

from pytoniq import LiteBalancer
from pytoniq.liteclient.client import LiteServerError
from pytoniq_core.tl import BlockIdExt

DATABASE = "ton"
MC_SHARD = -2 ** 63
TIMEOUT = 60
FLUSH_SECONDS = 2
FLUSH_ROWS = 20000
POLL_SECONDS = 1.0
# stay this many mc blocks behind the head: the public liteservers sit a few
# blocks apart, and the balancer would otherwise keep asking one that has not
# got the newest block yet ("not in db", code 651)
CONFIRMATIONS = 3
TRANSIENT_RETRY_SECONDS = 0.5
# touched after every mc block; the Docker HEALTHCHECK fails when it goes stale
HEARTBEAT = Path(os.environ.get("TON_HEARTBEAT_FILE", "/tmp/ton-blocks.heartbeat"))

# how long an outgoing message is remembered for linking its receiver into the
# sender's trace, in mc blocks (~30 min); traces practically finish in seconds
TRACE_KEEP = 3600

# The schema lives in migrations/NNNN_name.sql, applied in order on every start
# and recorded in ton.schema_migrations. Statements in a file end with ";" at the
# end of a line. A file is never edited once deployed: a change is a new file.
MIGRATIONS = Path(__file__).with_name("migrations")
# 0001-0006 are the schema as it stood when migrations were introduced
# (2026-10-21). A database that already has it is marked at that version
# instead of running them, so 0004 doesn't run against a filled account_state.
BASELINE = 6


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


def migrations():
    out = []
    for path in sorted(MIGRATIONS.glob("[0-9][0-9][0-9][0-9]_*.sql")):
        text = path.read_text(encoding="utf-8")
        stmts = [x.strip() for x in re.split(r";[ \t]*\n", text)]
        stmts = [x for x in stmts if re.sub(r"--[^\n]*", "", x).strip()]
        out.append((int(path.name[:4]), path.name, stmts))
    return out


def migrate():
    """Apply the migrations not yet recorded in schema_migrations, in order."""
    _post("CREATE TABLE IF NOT EXISTS %s.schema_migrations (version UInt32, name String,"
          " applied_at DateTime('UTC') DEFAULT now()) ENGINE = MergeTree ORDER BY version" % DATABASE)
    done = {int(x) for x in _post("SELECT version FROM %s.schema_migrations FORMAT TSV" % DATABASE).split()}
    todo = migrations()
    if not done and int(_post("EXISTS TABLE %s.transactions FORMAT TSV" % DATABASE).strip()):
        base = [(v, n) for v, n, _ in todo if v <= BASELINE]
        _insert("schema_migrations", [{"version": v, "name": n} for v, n in base])
        done = {v for v, _ in base}
        print("existing schema marked at migration %04d" % BASELINE, flush=True)
    for version, name, stmts in todo:
        if version in done:
            continue
        for stmt in stmts:
            _post(stmt)
        _insert("schema_migrations", [{"version": version, "name": name}])
        print("applied migration %s" % name, flush=True)


# op_names.tsv: hand-checked names for the most common ops (wins on overlap,
# the generator skips ops already in it). op_names_registry.tsv: generated by
# build_op_names.py from ton-blockchain/abis and tonkeeper/tongo.
OP_NAMES_FILES = [Path(__file__).with_name("op_names.tsv"),
                  Path(__file__).with_name("op_names_registry.tsv")]
def load_op_names():
    """Human names for op codes, from op_names.tsv next to this file (every
    entry carries the source it was looked up in). Re-inserted on every start;
    ReplacingMergeTree keeps the newest row per op, so editing the file and
    redeploying updates the names. op is stored like ops_hour.op: -1 = no body."""
    rows = []
    for path in OP_NAMES_FILES:
        with path.open(encoding="utf-8") as fh:
            header = fh.readline().rstrip("\n").split("\t")
            for line in fh:
                if not line.strip():
                    continue
                r = dict(zip(header, line.rstrip("\n").split("\t")))
                r["op"] = int(r["op"], 0)
                rows.append(r)
    _insert("op_names", rows)
    print("op_names: %d entries" % len(rows), flush=True)


CONTINUITY_EVERY = 300     # seconds between gap checks
CONTINUITY_SETTLE = 200    # mc blocks (~100 s) younger than this may still sit in the buffer
CONTINUITY_WINDOW = 3000   # how far back each check looks (~25 min)


def missing_mc_seqnos(lo, hi):
    """mc seqnos in [lo, hi) with no row in the destination table. Every mc
    block carries at least one non-tick-tock transaction (the fee-minting
    message from -1:000..), so an absent seqno is a lost one, not an empty one."""
    if hi <= lo:
        return []
    out = _post(
        "SELECT number FROM numbers({lo}, {n}) WHERE number NOT IN "
        "(SELECT DISTINCT mc_seqno FROM {db}.transactions WHERE mc_seqno >= {lo} AND mc_seqno < {hi}) "
        "ORDER BY number FORMAT TSV".format(db=DATABASE, lo=lo, hi=hi, n=hi - lo))
    return [int(x) for x in out.split()]


def resume_point():
    """The mc seqno after the last one stored, or None on an empty table."""
    # reading the Buffer table reads the destination plus whatever is still buffered
    out = _post("SELECT max(mc_seqno), count() FROM %s.transactions_buffer FORMAT TSV" % DATABASE)
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


def _msg_type(info):
    """internal / externalin, or empty for a transaction without an inbound message.

    The value must stay 'externalin': ton.minute_mv counts external messages by
    it. (Until 2026-10-21 08:47 UTC this wrote 'external', which the view never
    matched -- minute.external_in is 0 before that.)"""
    if info is None:
        return ""
    name = type(info).__name__
    return "internal" if name.startswith("Internal") else "externalin"


def trace_hex(trace_id):
    return "%064x" % trace_id


def tx_row(blk_id, mc_seqno, tx, trace_id=0):
    d = tx.description
    cph = getattr(d, "compute_ph", None)
    msg = tx.in_msg
    info = msg.info if msg is not None else None
    row = {
        "now": tx.now,
        "mc_seqno": mc_seqno,
        "workchain": blk_id.workchain,
        "block_seqno": blk_id.seqno,
        "account": "%d:%s" % (blk_id.workchain, tx.account_addr_hex),
        "lt": tx.lt,
        "in_msg_type": _msg_type(info),
        "src": _addr(getattr(info, "src", None)),
        "value": getattr(info, "value_coins", 0) or 0,
        "op": _op(msg) if msg is not None else None,
        "total_fees": tx.total_fees.grams,
        "success": int(not getattr(d, "aborted", False)),
        "exit_code": getattr(cph, "exit_code", None) if cph else None,
        "out_msgs": tx.outmsg_cnt,
        "orig_status": _status(tx.orig_status),
        "end_status": _status(tx.end_status),
        "trace_id": str(trace_id),   # UInt256 as a decimal string in JSON
    }
    return row


class Follower:
    def __init__(self, dry_run=False):
        self.client = None
        self.dry_run = dry_run
        self.prev_tops = None      # shard tops of the last processed mc block
        self.txs = []
        self.traces = {}           # out message hash -> (trace_id, mc seqno)
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

    async def one_block(self, blk_id, txs):
        """Collect one block's (block id, transaction) pairs; returns its header info (for the prev walk)."""
        hdr = await self.client.raw_get_block_header(blk_id)
        txs.extend((blk_id, t) for t in await self.client.raw_get_block_transactions_ext(blk_id))
        return hdr.info

    def trace_rows(self, mc_seqno, txs):
        """Rows for one mc block's transactions, each tagged with its trace.
        A child's lt is always above its parent's, so in lt order every parent
        in this mc block is linked before its children; parents from earlier mc
        blocks come from self.traces. Tick-tock transactions are linked (their
        messages start traces too) but not stored."""
        rows = []
        for blk_id, t in sorted(txs, key=lambda x: x[1].lt):
            msg = t.in_msg
            if msg is None or _msg_type(msg.info) == "externalin":
                trace = int.from_bytes(t.cell.hash, "big")
            else:
                trace = self.traces.pop(msg.serialize().hash, (0, 0))[0]
            for out in t.out_msgs:
                if _msg_type(out.info) == "internal":
                    self.traces[out.serialize().hash] = (trace, mc_seqno)
            if getattr(t.description, "type_", "") != "tick_tock":
                rows.append(tx_row(blk_id, mc_seqno, t, trace))
        if mc_seqno % 100 == 0:
            old = mc_seqno - TRACE_KEEP
            self.traces = {h: v for h, v in self.traces.items() if v[1] >= old}
        return rows

    def _covered(self, blk_id, prev_tops):
        return any(t.workchain == blk_id.workchain and _overlaps(t.shard, blk_id.shard)
                   and t.seqno >= blk_id.seqno for t in prev_tops)

    async def process(self, seqno):
        # rows reach the buffer only once the whole mc block succeeded: a retry
        # after a half-done block must not insert its first blocks twice (the
        # table would dedupe them on merge, the summary views would not)
        txs = []
        mc = await self.mc_id(seqno)
        await self.one_block(mc, txs)
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
            info = await self.one_block(b, txs)
            queue.extend(prev_ids(b, info))
        self.txs.extend(self.trace_rows(seqno, txs))
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
            _insert("transactions_buffer", self.txs)
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
    done, failures, transient, last_note = 0, 0, 0, time.monotonic()
    first, last_check = nxt, time.monotonic()
    while args.count is None or done < args.count:
        try:
            head = (await f.client.get_masterchain_info())["last"]["seqno"]
            if nxt > head - CONFIRMATIONS:
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
            if not args.dry_run and time.monotonic() - last_check >= CONTINUITY_EVERY:
                # rows a ClickHouse crash dropped from the buffer: fetch those mc blocks again
                last_check = time.monotonic()
                lost = missing_mc_seqnos(max(first, nxt - CONTINUITY_WINDOW), nxt - CONTINUITY_SETTLE)
                if lost:
                    print("refetching %d lost mc blocks (%d..%d)" % (len(lost), lost[0], lost[-1]), flush=True)
                    f.flush(force=True)
                    for seq in lost:
                        f.prev_tops = None
                        await f.process(seq)
                    f.prev_tops = None
                    f.flush(force=True)
        except Exception as e:
            if isinstance(e, LiteServerError) and e.code == 651:
                # a liteserver behind the others: just ask again (the balancer
                # picks another one); counted and reported once a minute
                transient += 1
                f.prev_tops = None
                if time.monotonic() - last_note >= 60:
                    print("%d transient 'not in db' retries in the last minute" % transient, flush=True)
                    transient, last_note = 0, time.monotonic()
                await asyncio.sleep(TRANSIENT_RETRY_SECONDS)
                continue
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
        for _, name, stmts in migrations():
            print("-- %s" % name)
            for stmt in stmts:
                print(stmt + ";\n")
        return
    if args.init or not args.dry_run:
        migrate()
        load_op_names()
        print("tables ready", flush=True)
        if args.init:
            return
    asyncio.run(run(args))


if __name__ == "__main__":
    main()
