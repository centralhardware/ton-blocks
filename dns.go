package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TON DNS record changes (TEP-81): the owner sends
//
//	change_dns_record#4eb1f0f9 query_id:uint64 key:uint256 value:(Maybe ^DNSRecord)
//
// to the domain's NFT item (.ton or t.me). No value deletes the record. Values:
//
//	dns_smc_address#9fd3      smc_addr:MsgAddressInt ...    -> "wallet"
//	dns_next_resolver#ba93    resolver:MsgAddressInt        -> "resolver"
//	dns_adnl_address#ad01     adnl_addr:bits256 ...         -> "adnl"
//	dns_storage_address#7473  bag_id:bits256                -> "storage"
//	dns_text#1eda             _:Text (chunked)              -> "text"
//
// The key is sha256 of the record name; the standard ones get their name.

type dnsRow struct {
	Now     uint32 `json:"now"`
	McSeqno uint32 `json:"mc_seqno"`
	TraceID string `json:"trace_id"`
	Item    string `json:"item"`
	Sender  string `json:"sender"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Success uint8  `json:"success"`
}

const opChangeDNSRecord = 0x4eb1f0f9

var dnsKeyNames = func() map[string]string {
	m := map[string]string{}
	for _, n := range []string{"wallet", "site", "storage", "dns_next_resolver"} {
		h := sha256.Sum256([]byte(n))
		m[hex.EncodeToString(h[:])] = n
	}
	return m
}()

// dnsChangeOf returns the DNS record change this transaction received, or nil.
func dnsChangeOf(tx *tlb.Transaction) (d *dnsRow) {
	defer func() {
		if recover() != nil {
			d = nil
		}
	}()
	if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeInternal {
		return nil
	}
	s, op := bodySlice(tx.IO.In)
	if op != opChangeDNSRecord {
		return nil
	}
	must(s.LoadUInt(64))
	key := hex.EncodeToString(must(s.LoadSlice(256)))
	d = &dnsRow{Key: key, Name: dnsKeyNames[key], Kind: "delete"}
	d.Sender = addrStr(tx.IO.In.Msg.SenderAddr())
	if must(s.LoadBoolBit()) {
		d.Kind, d.Value = dnsValue(must(s.LoadRef()))
	}
	return d
}

func dnsValue(s *cell.Slice) (string, string) {
	switch must(s.LoadUInt(16)) {
	case 0x9fd3:
		return "wallet", addrStr(must(s.LoadAddr()))
	case 0xba93:
		return "resolver", addrStr(must(s.LoadAddr()))
	case 0xad01:
		return "adnl", hex.EncodeToString(must(s.LoadSlice(256)))
	case 0x7473:
		return "storage", hex.EncodeToString(must(s.LoadSlice(256)))
	case 0x1eda:
		return "text", dnsText(s)
	}
	return "unknown", ""
}

// dnsText reads Text: chunks:(## 8) then that many chunks, each
// len:(## 8) data:(bits (len * 8)), the next one in a ref.
func dnsText(s *cell.Slice) string {
	n := must(s.LoadUInt(8))
	var b []byte
	for i := uint64(0); i < n; i++ {
		l := must(s.LoadUInt(8))
		b = append(b, must(s.LoadSlice(uint(l*8)))...)
		if i+1 < n {
			s = must(s.LoadRef())
		}
	}
	if len(b) > maxComment {
		b = b[:maxComment]
	}
	return strings.ToValidUTF8(string(b), "")
}
