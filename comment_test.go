package main

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func msgWith(body *cell.Cell) *tlb.Message {
	return &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{Body: body}}
}

func TestCommentText(t *testing.T) {
	long := strings.Repeat("я", 400) // 800 bytes, crosses cells
	text := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake(long).EndCell()
	addr := address.MustParseRawAddr("0:0007ea6f21153e666e6cffd956ddb704a12119bfe84827d5ff9523ecc18767d8")
	notify := func(inRef bool) *cell.Cell {
		b := cell.BeginCell().MustStoreUInt(0x7362d09c, 32).MustStoreUInt(1, 64).
			MustStoreBigVarUInt(tlb.MustFromTON("1").Nano(), 16).MustStoreAddr(addr).MustStoreBoolBit(inRef)
		payload := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("deposit 42")
		if inRef {
			return b.MustStoreRef(payload.EndCell()).EndCell()
		}
		return b.MustStoreBuilder(payload).EndCell()
	}
	for name, c := range map[string]struct {
		body *cell.Cell
		want string
	}{
		"text":        {text, strings.Repeat("я", 256)},
		"jetton ref":  {notify(true), "deposit 42"},
		"jetton here": {notify(false), "deposit 42"},
		"other op":    {cell.BeginCell().MustStoreUInt(5, 32).EndCell(), ""},
	} {
		if got := commentText(msgWith(c.body)); got != c.want {
			t.Errorf("%s: got %q (%d bytes)", name, got[:min(len(got), 40)], len(got))
		}
	}
}
