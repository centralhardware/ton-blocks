package main

import (
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// outRow is one message a transaction sent, as ton.out_messages stores it.
// Internal messages also show up as the receiver's in message, but only here
// do bounced-back and never-delivered ones appear, and external-out messages
// (contract events/logs) appear nowhere else.
type outRow struct {
	Now      uint32  `json:"now"`
	McSeqno  uint32  `json:"mc_seqno"`
	TraceID  string  `json:"trace_id"`
	Account  string  `json:"account"`
	LT       uint64  `json:"lt"`
	Idx      uint16  `json:"idx"`
	Type     string  `json:"type"` // internal | externalout
	Dest     string  `json:"dest"` // '' for an external-out without one
	Value    uint64  `json:"value"`
	Op       *uint32 `json:"op"`
	Bounce   uint8   `json:"bounce"`
	Bounced  uint8   `json:"bounced"`
	FwdFee   uint64  `json:"fwd_fee"`
	Comment  string  `json:"comment"`
	EventBOC string  `json:"event_boc"` // external-out body, base64 BOC
}

// bodyRow is the raw body of a transaction's incoming message.
type bodyRow struct {
	Now     uint32 `json:"now"`
	Account string `json:"account"`
	LT      uint64 `json:"lt"`
	BOC     string `json:"boc"`
}

// maxBody skips storing a body larger than this (base64 bytes); they're rare
// and mostly code deployments, whose code is in ton.code anyway.
const maxBody = 16 << 10

func boc(c *cell.Cell) string {
	if c == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(c.ToBOC())
}

func outRows(tx *tlb.Transaction, account string, mcSeqno uint32, trace *big.Int) []outRow {
	var rows []outRow
	for i, m := range outMessages(tx) {
		r := outRow{Now: tx.Now, McSeqno: mcSeqno, TraceID: trace.String(), Account: account,
			LT: tx.LT, Idx: uint16(i), Op: opOf(&m), Comment: commentText(&m)}
		switch m.MsgType {
		case tlb.MsgTypeInternal:
			im := m.AsInternal()
			r.Type, r.Dest, r.Value = "internal", addrStr(im.DstAddr), im.Amount.Nano().Uint64()
			r.FwdFee = im.FwdFee.Nano().Uint64()
			r.Bounce, r.Bounced = b2u(im.Bounce), b2u(im.Bounced)
		case tlb.MsgTypeExternalOut:
			em := m.AsExternalOut()
			r.Type = "externalout"
			if em.DstAddr != nil && em.DstAddr.Type() == address.ExtAddress {
				r.Dest = fmt.Sprintf("%x", em.DstAddr.Data())
			}
			if s := boc(em.Body); len(s) <= maxBody {
				r.EventBOC = s
			}
		default:
			continue
		}
		rows = append(rows, r)
	}
	return rows
}

func b2u(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// fees fills the per-phase fee columns and gas use.
func fees(tx *tlb.Transaction, r *row) {
	if f := descField(tx.Description, "StoragePhase"); f.IsValid() {
		switch sp := f.Interface().(type) {
		case *tlb.StoragePhase:
			if sp != nil {
				r.StorageFees = sp.StorageFeesCollected.Nano().Uint64()
			}
		case tlb.StoragePhase:
			r.StorageFees = sp.StorageFeesCollected.Nano().Uint64()
		}
	}
	if f := descField(tx.Description, "ComputePhase"); f.IsValid() {
		if cp, ok := f.Interface().(tlb.ComputePhase); ok {
			var vm *tlb.ComputePhaseVM
			switch p := cp.Phase.(type) {
			case tlb.ComputePhaseVM:
				vm = &p
			case *tlb.ComputePhaseVM:
				vm = p
			}
			if vm != nil {
				r.GasFees = vm.GasFees.Nano().Uint64()
				r.GasUsed = vm.Details.GasUsed.Uint64()
				r.VMSteps = vm.Details.VMSteps
			}
		}
	}
	if f := descField(tx.Description, "ActionPhase"); f.IsValid() {
		if ap, ok := f.Interface().(*tlb.ActionPhase); ok && ap != nil {
			if ap.TotalFwdFees != nil {
				r.FwdFees = ap.TotalFwdFees.Nano().Uint64()
			}
			if ap.TotalActionFees != nil {
				r.ActionFees = ap.TotalActionFees.Nano().Uint64()
			}
		}
	}
}

// initCodeHash is the code hash (base64, as in ton.code) of the StateInit an
// incoming message carries, i.e. the code a deployment installs; ” if none.
func initCodeHash(m *tlb.Message) string {
	var si *tlb.StateInit
	switch m.MsgType {
	case tlb.MsgTypeInternal:
		si = m.AsInternal().StateInit
	case tlb.MsgTypeExternalIn:
		si = m.AsExternalIn().StateInit
	}
	if si == nil || si.Code == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(si.Code.Hash())
}
