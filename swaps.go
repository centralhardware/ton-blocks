package main

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// DEX swaps, decoded from the pool's own transaction (layouts from
// ton-blockchain/abis, data/dedust and data/stonfi):
//
//	DeDust            the pool emits an external SwapEvent 0x9c610de3 with both
//	                  assets (jetton masters, or native TON) and both amounts
//	STON.fi v1        in: swap 0x25938561 (input router wallet + amount),
//	                  out: pay_to 0xf93bb43f (amounts and router wallets of both sides)
//	STON.fi v2        in: swap 0x6664de2a (left/right input amounts),
//	                  out: pay_to 0x657b54f5 (as v1, in a ref)
//
// STON.fi names tokens by the router's jetton wallet, not the master; the
// board maps them through jetton_wallets. token = '' is native TON (DeDust).

type swapRow struct {
	Now       uint32 `json:"now"`
	McSeqno   uint32 `json:"mc_seqno"`
	TraceID   string `json:"trace_id"`
	Dex       string `json:"dex"`
	Pool      string `json:"pool"`
	User      string `json:"user"`
	TokenIn   string `json:"token_in"`
	AmountIn  string `json:"amount_in"`
	TokenOut  string `json:"token_out"`
	AmountOut string `json:"amount_out"`
}

const (
	opDedustSwapEvent = 0x9c610de3
	opStonV1Swap      = 0x25938561
	opStonV1PayTo     = 0xf93bb43f
	opStonV2Swap      = 0x6664de2a
	opStonV2PayTo     = 0x657b54f5
)

func addrStr(a *address.Address) string {
	if a == nil || a.Type() != address.StdAddress {
		return ""
	}
	return a.StringRaw()
}

func bodySlice(m *tlb.Message) (*cell.Slice, uint64) {
	body := m.Msg.Payload()
	if body == nil {
		return nil, 0
	}
	s, err := body.BeginParse()
	if err != nil || s.BitsLeft() < 32 {
		return nil, 0
	}
	op, _ := s.LoadUInt(32)
	return s, op
}

func outMessages(tx *tlb.Transaction) []tlb.Message {
	if tx.IO.Out == nil {
		return nil
	}
	list, err := tx.IO.Out.ToSlice()
	if err != nil {
		return nil
	}
	return list
}

// swapOf returns the swap this transaction executed, or nil. A failed or
// refunded swap is not one.
func swapOf(tx *tlb.Transaction) (sw *swapRow) {
	defer func() {
		if recover() != nil { // a body that doesn't match the layout
			sw = nil
		}
	}()
	out := outMessages(tx)
	for i := range out {
		if out[i].MsgType != tlb.MsgTypeExternalOut {
			continue
		}
		if s, op := bodySlice(&out[i]); op == opDedustSwapEvent {
			return dedustSwap(s)
		}
	}
	if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeInternal {
		return nil
	}
	in, op := bodySlice(tx.IO.In)
	switch op {
	case opStonV1Swap:
		return stonV1Swap(in, out)
	case opStonV2Swap:
		return stonV2Swap(in, out)
	}
	return nil
}

func dedustAsset(s *cell.Slice) string {
	switch must(s.LoadUInt(4)) {
	case 0:
		return ""
	case 1:
		wc := must(s.LoadInt(8))
		return fmt.Sprintf("%d:%x", wc, must(s.LoadSlice(256)))
	}
	panic("unknown asset")
}

func dedustSwap(s *cell.Slice) *swapRow {
	r := &swapRow{Dex: "dedust"}
	r.TokenIn = dedustAsset(s)
	r.TokenOut = dedustAsset(s)
	r.AmountIn = must(s.LoadBigCoins()).String()
	r.AmountOut = must(s.LoadBigCoins()).String()
	tail := must(s.LoadRef())
	r.User = addrStr(must(tail.LoadAddr()))
	return r
}

// payTo is a STON.fi payout: both sides' amounts and router wallets.
type payTo struct {
	amount0, amount1 *big.Int
	token0, token1   string
}

func loadPayTo(s *cell.Slice) payTo {
	must(s.LoadBigCoins())
	return payToAmounts(s)
}

func payToAmounts(s *cell.Slice) payTo {
	var p payTo
	p.amount0 = must(s.LoadBigCoins())
	p.token0 = addrStr(must(s.LoadAddr()))
	p.amount1 = must(s.LoadBigCoins())
	p.token1 = addrStr(must(s.LoadAddr()))
	return p
}

func findPayTo(out []tlb.Message, op uint64, parse func(*cell.Slice) payTo) (payTo, bool) {
	for i := range out {
		if out[i].MsgType != tlb.MsgTypeInternal {
			continue
		}
		if s, o := bodySlice(&out[i]); o == op {
			return parse(s), true
		}
	}
	return payTo{}, false
}

func stonV1Swap(in *cell.Slice, out []tlb.Message) *swapRow {
	must(in.LoadUInt(64))
	user := addrStr(must(in.LoadAddr()))
	wallet := addrStr(must(in.LoadAddr()))
	amount := must(in.LoadBigCoins())
	p, ok := findPayTo(out, opStonV1PayTo, func(s *cell.Slice) payTo {
		must(s.LoadUInt(64))
		must(s.LoadAddr())
		must(s.LoadUInt(32))       // exit code
		if must(s.LoadBoolBit()) { // Either inline ^ref
			s = must(s.LoadRef())
		}
		return payToAmounts(s)
	})
	if !ok {
		return nil
	}
	r := &swapRow{Dex: "stonfi_v1", User: user, TokenIn: wallet, AmountIn: amount.String()}
	switch {
	case p.amount0.Sign() > 0 && p.token0 != wallet:
		r.TokenOut, r.AmountOut = p.token0, p.amount0.String()
	case p.amount1.Sign() > 0 && p.token1 != wallet:
		r.TokenOut, r.AmountOut = p.token1, p.amount1.String()
	default:
		return nil // a refund pays back the input token
	}
	return r
}

func stonV2Swap(in *cell.Slice, out []tlb.Message) *swapRow {
	must(in.LoadUInt(64))
	user := addrStr(must(in.LoadAddr()))
	left := must(in.LoadBigCoins())
	right := must(in.LoadBigCoins())
	p, ok := findPayTo(out, opStonV2PayTo, func(s *cell.Slice) payTo {
		must(s.LoadUInt(64))
		must(s.LoadAddr())
		must(s.LoadAddr())
		must(s.LoadAddr())
		must(s.LoadUInt(32))
		must(s.LoadMaybeRef())
		return loadPayTo(must(s.LoadRef()))
	})
	if !ok {
		return nil
	}
	r := &swapRow{Dex: "stonfi_v2", User: user}
	if left.Sign() > 0 && p.amount1.Sign() > 0 {
		r.TokenIn, r.AmountIn, r.TokenOut, r.AmountOut = p.token0, left.String(), p.token1, p.amount1.String()
	} else if right.Sign() > 0 && p.amount0.Sign() > 0 {
		r.TokenIn, r.AmountIn, r.TokenOut, r.AmountOut = p.token1, right.String(), p.token0, p.amount0.String()
	} else {
		return nil // refund
	}
	return r
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
