package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// valueFlow is a block's ValueFlow, in nanoton (extra currencies ignored):
// what came from the previous block and went to the next, imported/exported
// with messages, fees collected (and, since value_flow_v2, burned), and on the
// masterchain the fees imported from shards, recovered fees, created (validator
// reward) and minted coins.
type valueFlow struct {
	FromPrev      uint64 `json:"from_prev"`
	ToNext        uint64 `json:"to_next"`
	Imported      uint64 `json:"imported"`
	Exported      uint64 `json:"exported"`
	FeesCollected uint64 `json:"fees_collected"`
	Burned        uint64 `json:"burned"`
	FeesImported  uint64 `json:"fees_imported"`
	Recovered     uint64 `json:"recovered"`
	Created       uint64 `json:"created"`
	Minted        uint64 `json:"minted"`
}

// grams reads a CurrencyCollection and returns its nanoton part.
func grams(s *cell.Slice) (uint64, error) {
	v, err := s.LoadBigCoins()
	if err != nil {
		return 0, err
	}
	if _, err := s.LoadMaybeRef(); err != nil { // extra currencies dict
		return 0, err
	}
	return v.Uint64(), nil
}

func loadGrams(s *cell.Slice, dst ...*uint64) error {
	for _, d := range dst {
		v, err := grams(s)
		if err != nil {
			return err
		}
		*d = v
	}
	return nil
}

func parseValueFlow(c *cell.Cell) (vf valueFlow, err error) {
	if c == nil {
		return vf, fmt.Errorf("no value flow")
	}
	s := c.MustBeginParse()
	tag, err := s.LoadUInt(32)
	if err != nil {
		return vf, err
	}
	if tag != 0xb8e48dfb && tag != 0x3ebf98b7 {
		return vf, fmt.Errorf("value flow tag %08x", tag)
	}
	r1, err := s.LoadRef()
	if err != nil {
		return vf, err
	}
	if err := loadGrams(r1, &vf.FromPrev, &vf.ToNext, &vf.Imported, &vf.Exported); err != nil {
		return vf, err
	}
	if err := loadGrams(s, &vf.FeesCollected); err != nil {
		return vf, err
	}
	if tag == 0x3ebf98b7 {
		if err := loadGrams(s, &vf.Burned); err != nil {
			return vf, err
		}
	}
	r2, err := s.LoadRef()
	if err != nil {
		return vf, err
	}
	err = loadGrams(r2, &vf.FeesImported, &vf.Recovered, &vf.Created, &vf.Minted)
	return vf, err
}

// nodeID is a validator's short node id: sha256 of its TL-serialized ed25519
// public key (pub.ed25519 = 0x4813b4c6). Block signatures name signers by it.
func nodeID(pubkey []byte) string {
	b := make([]byte, 4, 36)
	binary.LittleEndian.PutUint32(b, 0x4813b4c6)
	h := sha256.Sum256(append(b, pubkey...))
	return string(h[:])
}

// vset is the validator set signatures are matched against.
type vset struct {
	since, until uint32
	main         uint16            // the first main validators sign mc blocks
	idx          map[string]uint16 // node id -> index in the set
}

// sigKey is one validator's row in ton.validator_signatures.
type sigKey struct {
	hour  uint32
	since uint32
	idx   uint16
}

type sigCount struct{ signed, sampled uint32 }

type sigRow struct {
	Hour       uint32 `json:"hour"`
	UTimeSince uint32 `json:"utime_since"`
	Idx        uint16 `json:"idx"`
	Signed     uint32 `json:"signed"`
	Sampled    uint32 `json:"sampled"`
}

// Mc blocks don't carry their signatures (prev_blk_signatures is empty on
// mainnet); they come with a block proof, one liteserver request per block.
// So only every sigEvery-th mc block is checked, sigBehind blocks back: public
// liteservers lag by dozens of blocks at times, and a proof for a block newer
// than the server's own tip fails.
const (
	sigEvery  = 10
	sigBehind = 100
)

func (f *follower) loadVset(ctx context.Context, mc *ton.BlockIDExt) error {
	cfg, err := f.api.GetBlockchainConfig(ctx, mc, 34)
	if err != nil {
		return err
	}
	rows, err := validatorSet(cfg.Get(34), 34)
	if err != nil {
		return err
	}
	v := &vset{idx: map[string]uint16{}}
	for _, r := range rows {
		key, _ := hex.DecodeString(r.PubKey)
		v.idx[nodeID(key)] = r.Idx
		v.since, v.until = r.UTimeSince, r.UTimeUntil
		if r.Main {
			v.main++
		}
	}
	f.vset = v
	return nil
}

// countSignatures samples mc block seqno-sigBehind: which main validators
// signed it, added to f.sigs. Errors only cost a sample.
func (f *follower) countSignatures(ctx context.Context, seqno uint32) error {
	if seqno%sigEvery != 0 || seqno <= sigBehind {
		return nil
	}
	target, err := f.mcID(ctx, seqno-sigBehind)
	if err != nil {
		return err
	}
	prev, err := f.mcID(ctx, seqno-sigBehind-1)
	if err != nil {
		return err
	}
	hdr, err := f.api.GetBlockData(ctx, target)
	if err != nil {
		return err
	}
	now := hdr.BlockInfo.GenUtime
	if f.vset == nil || now >= f.vset.until || now < f.vset.since {
		if err := f.loadVset(ctx, target); err != nil {
			return err
		}
	}
	proof, err := f.api.WithRetry(3).GetBlockProof(ctx, prev, target)
	if err != nil {
		return err
	}
	var sigs []ton.Signature
	for _, st := range proof.Steps {
		if fw, ok := st.(ton.BlockLinkForward); ok {
			switch ss := fw.SignatureSet.(type) {
			case ton.SignatureSet:
				sigs = ss.Signatures
			case ton.SignatureSetOrdinary:
				sigs = ss.Signatures
			case ton.SignatureSetSimplex:
				sigs = ss.Signatures
			}
		}
	}
	if sigs == nil {
		return fmt.Errorf("mc %d: no signatures in proof", target.SeqNo)
	}
	hour := now - now%3600
	for i := uint16(0); i < f.vset.main; i++ {
		k := sigKey{hour, f.vset.since, i}
		c := f.sigs[k]
		c.sampled++
		f.sigs[k] = c
	}
	for _, sg := range sigs {
		if i, ok := f.vset.idx[string(sg.NodeIDShort)]; ok {
			k := sigKey{hour, f.vset.since, i}
			c := f.sigs[k]
			c.signed++
			f.sigs[k] = c
		}
	}
	return nil
}

func (f *follower) sigRows() []sigRow {
	rows := make([]sigRow, 0, len(f.sigs))
	for k, c := range f.sigs {
		rows = append(rows, sigRow{k.hour, k.since, k.idx, c.signed, c.sampled})
	}
	return rows
}

// configRow is one config param as a key block carried it, stored when its
// hash differs from the last key block's.
type configRow struct {
	McSeqno uint32 `json:"mc_seqno"`
	Now     uint32 `json:"now"`
	Param   int32  `json:"param"`
	Hash    string `json:"hash"`
	Size    int    `json:"size"`
}

func (f *follower) configChanges(mcSeqno uint32, data *tlb.Block) error {
	if data.Extra == nil || data.Extra.Custom == nil || data.Extra.Custom.ConfigParams == nil {
		return nil
	}
	kvs, err := data.Extra.Custom.ConfigParams.Config.Params.LoadAll()
	if err != nil {
		return err
	}
	for _, kv := range kvs {
		p, err := kv.Key.LoadInt(32)
		if err != nil {
			return err
		}
		ref, err := kv.Value.LoadRef()
		if err != nil {
			return err
		}
		c, err := ref.ToCell()
		if err != nil {
			return err
		}
		h := base64.StdEncoding.EncodeToString(c.Hash())
		if f.cfgHashes[int32(p)] == h {
			continue
		}
		f.cfgHashes[int32(p)] = h
		f.configs = append(f.configs, configRow{mcSeqno, data.BlockInfo.GenUtime, int32(p), h, len(c.ToBOC())})
	}
	return nil
}

// txType names a transaction's description kind.
func txType(tx *tlb.Transaction) string {
	switch d := tx.Description.(type) {
	case tlb.TransactionDescriptionOrdinary, *tlb.TransactionDescriptionOrdinary:
		return "ord"
	case tlb.TransactionDescriptionTickTock:
		return map[bool]string{false: "tick", true: "tock"}[d.IsTock]
	case *tlb.TransactionDescriptionTickTock:
		return map[bool]string{false: "tick", true: "tock"}[d.IsTock]
	case tlb.TransactionDescriptionStorage, *tlb.TransactionDescriptionStorage:
		return "storage"
	case tlb.TransactionDescriptionSplitPrepare, *tlb.TransactionDescriptionSplitPrepare:
		return "split_prepare"
	case tlb.TransactionDescriptionSplitInstall, *tlb.TransactionDescriptionSplitInstall:
		return "split_install"
	case tlb.TransactionDescriptionMergePrepare, *tlb.TransactionDescriptionMergePrepare:
		return "merge_prepare"
	case tlb.TransactionDescriptionMergeInstall, *tlb.TransactionDescriptionMergeInstall:
		return "merge_install"
	}
	return ""
}

// details fills the compute and action phase details and the destroyed flag.
func details(tx *tlb.Transaction, r *row) {
	if d := descField(tx.Description, "Destroyed"); d.IsValid() && d.Bool() {
		r.Destroyed = 1
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
				d := vm.Details
				r.GasLimit = d.GasLimit.Uint64()
				if d.GasCredit != nil {
					r.GasCredit = d.GasCredit.Uint64()
				}
				r.ExitArg = d.ExitArg
			}
		}
	}
	if f := descField(tx.Description, "ActionPhase"); f.IsValid() {
		if ap, ok := f.Interface().(*tlb.ActionPhase); ok && ap != nil {
			r.TotalActions, r.SkippedActions, r.MsgsCreated = ap.TotalActions, ap.SkippedActions, ap.MessagesCreated
			r.MsgCells, r.MsgBits = ap.TotalMsgSize.Cells.Uint64(), ap.TotalMsgSize.Bits.Uint64()
		}
	}
}

// extraCurrencies decodes an ExtraCurrencyCollection dict: id -> amount.
func extraCurrencies(d *cell.Dictionary) map[string]string {
	m := map[string]string{}
	if d == nil {
		return m
	}
	kvs, err := d.LoadAll()
	if err != nil {
		return m
	}
	for _, kv := range kvs {
		id, err1 := kv.Key.LoadUInt(32)
		v, err2 := kv.Value.LoadVarUInt(32)
		if err1 == nil && err2 == nil {
			m[fmt.Sprint(id)] = v.String()
		}
	}
	return m
}

func hashInt(b []byte) string { return new(big.Int).SetBytes(b).String() }

// loadConfigHashes seeds f.cfgHashes from the stored changes, so a restart
// doesn't record every param again.
func (f *follower) loadConfigHashes() error {
	out, err := post("SELECT param, argMax(hash, mc_seqno) FROM "+database+".config_changes GROUP BY param FORMAT TSV", nil)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var p int32
		var h string
		if _, err := fmt.Sscan(line, &p, &h); err == nil {
			f.cfgHashes[p] = h
		}
	}
	return nil
}
