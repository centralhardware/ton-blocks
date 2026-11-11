package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Sequential trace numbers (trace_seq, see migrations/0023_trace_seq.sql).

// traceSeqTables carry trace_seq next to trace_id.
var traceSeqTables = []string{"transactions", "out_messages", "swaps", "dns_changes"}

// queryUint runs a query returning one unsigned number.
func queryUint(q string) (uint64, error) {
	out, err := post(q+" FORMAT TSV", nil)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(out)
	if s == "" || s == `\N` {
		return 0, nil
	}
	return strconv.ParseUint(s, 10, 64)
}

// maxTraceSeq is the last number handed out (0 on an empty table). Roots are
// always in transactions; reading the buffer covers rows still in it.
func maxTraceSeq() (uint64, error) {
	return queryUint("SELECT max(trace_seq) FROM " + database + "." + insertTable)
}

// waitMutations blocks until no mutation on ton.* is running.
func waitMutations() error {
	for {
		n, err := queryUint("SELECT count() FROM system.mutations WHERE database = '" + database + "' AND NOT is_done")
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
}

// backfillTraceSeq numbers the rows stored before trace_seq existed, one day at
// a time (a day's traces fit in memory as a Join table; the whole history
// doesn't), oldest first, then puts the 1-day TTL on trace_id. A trace belongs
// to the day of its first transaction; its later rows (traces last seconds,
// 74 s at most in 2026-10/11) are caught by updating two days. Safe to
// interrupt: it resumes at the first day with unnumbered rows.
func backfillTraceSeq() error {
	if err := waitMutations(); err != nil {
		return err
	}
	for {
		day, err := queryUint("SELECT toUInt32(toDate(min(now))) FROM " + database +
			".transactions WHERE trace_seq = 0 AND trace_id != 0")
		if err != nil {
			return err
		}
		if day == 0 {
			break
		}
		base, err := maxTraceSeq()
		if err != nil {
			return err
		}
		from := fmt.Sprintf("toDateTime(toDate(%d), 'UTC')", day)
		stmts := []string{
			"DROP TABLE IF EXISTS " + database + ".trace_seq_map",
			"CREATE TABLE " + database + ".trace_seq_map (trace_id UInt256, seq UInt64) ENGINE = Join(ANY, LEFT, trace_id)",
			fmt.Sprintf(`INSERT INTO %s.trace_seq_map
SELECT trace_id, %d + row_number() OVER (ORDER BY first, trace_id) FROM (
    SELECT trace_id, min(now) AS first FROM %s.transactions
    WHERE trace_seq = 0 AND trace_id != 0 AND now >= %s AND now < %s + INTERVAL 2 DAY
    GROUP BY trace_id HAVING first < %s + INTERVAL 1 DAY)`, database, base, database, from, from, from),
		}
		for _, t := range traceSeqTables {
			stmts = append(stmts, fmt.Sprintf(`ALTER TABLE %s.%s UPDATE trace_seq = joinGet('%s.trace_seq_map', 'seq', trace_id)
WHERE trace_seq = 0 AND trace_id != 0 AND now >= %s AND now < %s + INTERVAL 2 DAY`, database, t, database, from, from))
		}
		for _, s := range stmts {
			if _, err := post(s, nil); err != nil {
				return err
			}
		}
		if err := waitMutations(); err != nil {
			return err
		}
		top, _ := maxTraceSeq()
		log.Printf("trace_seq backfill: day %s numbered (up to %d)", time.Unix(int64(day)*86400, 0).UTC().Format("2006-01-02"), top)
	}
	if _, err := post("DROP TABLE IF EXISTS "+database+".trace_seq_map", nil); err != nil {
		return err
	}
	// the buffers sit on these two and can't ALTER; the TTL is metadata only
	// and the buffer keeps the destination's columns
	for _, t := range []string{"transactions", "out_messages"} {
		if _, err := post("ALTER TABLE "+database+"."+t+" MODIFY COLUMN trace_id UInt256 TTL now + INTERVAL 1 DAY", nil); err != nil {
			return err
		}
	}
	return nil
}
