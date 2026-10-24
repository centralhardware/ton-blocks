package main

import (
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestDNSChange(t *testing.T) {
	owner := address.MustParseRawAddr("0:07f1f7df722d3cee5d7131ad693119d13cb860a0ed409d034a7cfb76f214cc2b")
	change := func(key string, value *cell.Cell) *tlb.Transaction {
		b := cell.BeginCell().MustStoreUInt(opChangeDNSRecord, 32).MustStoreUInt(7, 64)
		for k, n := range dnsKeyNames {
			if n == key {
				b.MustStoreSlice(must(hex.DecodeString(k)), 256)
			}
		}
		b.MustStoreBoolBit(value != nil)
		if value != nil {
			b.MustStoreRef(value)
		}
		in := &tlb.Message{MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{SrcAddr: owner, Body: b.EndCell()}}
		return &tlb.Transaction{IO: struct {
			In  *tlb.Message      `tlb:"maybe ^"`
			Out *tlb.MessagesList `tlb:"maybe ^"`
		}{In: in}}
	}
	wallet := cell.BeginCell().MustStoreUInt(0x9fd3, 16).MustStoreAddr(owner).MustStoreUInt(0, 8).EndCell()
	text := cell.BeginCell().MustStoreUInt(0x1eda, 16).MustStoreUInt(2, 8).
		MustStoreUInt(3, 8).MustStoreSlice([]byte("abc"), 24).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 8).MustStoreSlice([]byte("de"), 16).EndCell()).EndCell()
	for name, c := range map[string]struct {
		tx               *tlb.Transaction
		key, kind, value string
	}{
		"wallet": {change("wallet", wallet), "wallet", "wallet", owner.StringRaw()},
		"text":   {change("site", text), "site", "text", "abcde"},
		"delete": {change("storage", nil), "storage", "delete", ""},
	} {
		d := dnsChangeOf(c.tx)
		if d == nil || d.Name != c.key || d.Kind != c.kind || d.Value != c.value || d.Sender != owner.StringRaw() {
			t.Errorf("%s: got %+v", name, d)
		}
	}
}
