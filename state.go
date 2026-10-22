package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Balances, NFT ownership and the validator set are chain state, not in the
// blocks. Balances and NFTs come from toncenter's index (sharing the 1 rps
// limiter with the token and contract loops); the validator set is config
// params 34/36, read from the liteservers.
const (
	stateEvery     = 5 * time.Minute
	stateLookback  = 3600 // seconds: accounts that transacted since are refreshed
	nftLookback    = 7 * 24 * 3600
	validatorEvery = 10 * time.Minute
)

type balanceRow struct {
	Account string `json:"account"`
	Balance uint64 `json:"balance"`
	At      uint32 `json:"at"`
}

// staleBalances: accounts whose last transaction is newer than their stored
// balance, so a balance is re-read once per burst of activity, not per tx.
func staleBalances() ([]string, error) {
	out, err := post(fmt.Sprintf("SELECT t.account FROM (SELECT account, max(now) AS last FROM %[1]s.transactions "+
		"WHERE now > now() - %[2]d GROUP BY account) t LEFT JOIN (SELECT account, max(at) AS at FROM %[1]s.balances "+
		"GROUP BY account) b ON b.account = t.account WHERE t.last > b.at LIMIT %[3]d FORMAT TSV",
		database, stateLookback, contractQuery), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func lookupBalances(accounts []string) ([]balanceRow, error) {
	var body struct {
		Accounts []struct {
			Address string
			Balance string
		} `json:"accounts"`
	}
	q := url.Values{"address": accounts, "include_boc": {"false"}}
	if err := toncenterGet("accountStates", q, &body); err != nil {
		return nil, err
	}
	now := uint32(time.Now().Unix())
	var res []balanceRow
	for _, a := range body.Accounts {
		if b, err := strconv.ParseUint(a.Balance, 10, 64); err == nil {
			res = append(res, balanceRow{strings.ToLower(a.Address), b, now})
		}
	}
	return res, nil
}

type nftRow struct {
	Item       string `json:"item"`
	Collection string `json:"collection"`
	Owner      string `json:"owner"`
	Index      string `json:"idx"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	OnSale     bool   `json:"on_sale"`
	At         uint32 `json:"at"`
}

type nftCollectionRow struct {
	Collection string `json:"collection"`
	Name       string `json:"name"`
	Image      string `json:"image"`
}

// staleNFTs: items that received an NFT transfer (0x5fcc3d14) since we last
// looked them up; ownership changes on exactly those.
func staleNFTs() ([]string, error) {
	out, err := post(fmt.Sprintf("SELECT t.account FROM (SELECT account, max(now) AS last FROM %[1]s.transactions "+
		"WHERE now > now() - %[2]d AND op = 0x5fcc3d14 AND success GROUP BY account) t LEFT JOIN "+
		"(SELECT item, max(at) AS at FROM %[1]s.nft_items GROUP BY item) n ON n.item = t.account "+
		"WHERE t.last > n.at LIMIT %[3]d FORMAT TSV", database, nftLookback, contractQuery), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

type tokenInfo struct {
	Valid bool
	Type  string
	Name  string
	Image string
}

func lookupNFTs(items []string) ([]nftRow, []nftCollectionRow, error) {
	var body struct {
		Items []struct {
			Address    string
			Index      string
			Collection string `json:"collection_address"`
			Owner      string `json:"real_owner"`
			OnSale     bool   `json:"on_sale"`
		} `json:"nft_items"`
		Metadata map[string]struct {
			TokenInfo []tokenInfo `json:"token_info"`
		}
	}
	q := url.Values{"address": items, "limit": {strconv.Itoa(len(items))}}
	if err := toncenterGet("nft/items", q, &body); err != nil {
		return nil, nil, err
	}
	meta := func(addr string) (name, image string) {
		for _, t := range body.Metadata[addr].TokenInfo {
			if t.Valid {
				return t.Name, t.Image
			}
		}
		return "", ""
	}
	now := uint32(time.Now().Unix())
	var res []nftRow
	var cols []nftCollectionRow
	seen := map[string]bool{}
	for _, it := range body.Items {
		name, image := meta(it.Address)
		res = append(res, nftRow{strings.ToLower(it.Address), strings.ToLower(it.Collection),
			strings.ToLower(it.Owner), it.Index, name, image, it.OnSale, now})
		if c := it.Collection; c != "" && !seen[c] {
			seen[c] = true
			if name, image := meta(c); name != "" {
				cols = append(cols, nftCollectionRow{strings.ToLower(c), name, image})
			}
		}
	}
	return res, cols, nil
}

// resolveState runs forever next to the follower, like resolveContracts: one
// INSERT per round per table.
func resolveState() {
	for {
		accounts, err := staleBalances()
		var bals []balanceRow
		for i := 0; err == nil && i < len(accounts); i += contractBatch {
			var bs []balanceRow
			if bs, err = lookupBalances(accounts[i:min(i+contractBatch, len(accounts))]); err == nil {
				bals = append(bals, bs...)
			}
		}
		if ierr := insert("balances", bals); ierr != nil && err == nil {
			err = ierr
		}
		if err != nil {
			log.Printf("balance lookup: %v", err)
		}

		items, err := staleNFTs()
		var nfts []nftRow
		var cols []nftCollectionRow
		for i := 0; err == nil && i < len(items); i += contractBatch {
			var ns []nftRow
			var cs []nftCollectionRow
			if ns, cs, err = lookupNFTs(items[i:min(i+contractBatch, len(items))]); err == nil {
				nfts, cols = append(nfts, ns...), append(cols, cs...)
			}
		}
		if ierr := insert("nft_items", nfts); ierr != nil && err == nil {
			err = ierr
		}
		if ierr := insert("nft_collections", cols); ierr != nil && err == nil {
			err = ierr
		}
		if err != nil {
			log.Printf("nft lookup: %v", err)
		}
		time.Sleep(stateEvery)
	}
}

type validatorRow struct {
	Param      uint8   `json:"param"` // 34 current, 36 next
	UTimeSince uint32  `json:"utime_since"`
	UTimeUntil uint32  `json:"utime_until"`
	Idx        uint16  `json:"idx"`
	PubKey     string  `json:"pubkey"` // hex, as block_log.created_by
	Weight     uint64  `json:"weight"`
	Share      float64 `json:"share"`
	ADNL       string  `json:"adnl"`
	Main       bool    `json:"main"` // among the masterchain validators
}

// validatorSet decodes config param 34 or 36 (ValidatorSet / ValidatorSetExt).
func validatorSet(c *cell.Cell, param uint8) ([]validatorRow, error) {
	var vs tlb.ValidatorSetAny
	if err := tlb.LoadFromCell(&vs, c.MustBeginParse()); err != nil {
		return nil, err
	}
	var since, until uint32
	var main uint16
	var list *cell.Dictionary
	switch v := vs.Validators.(type) {
	case tlb.ValidatorSet:
		since, until, main, list = v.UTimeSince, v.UTimeUntil, v.Main, v.List
	case tlb.ValidatorSetExt:
		since, until, main, list = v.UTimeSince, v.UTimeUntil, v.Main, v.List
	default:
		return nil, fmt.Errorf("unknown validator set %T", v)
	}
	kvs, err := list.LoadAll()
	if err != nil {
		return nil, err
	}
	var rows []validatorRow
	var total uint64
	for _, kv := range kvs {
		idx, _ := kv.Key.LoadUInt(16)
		s := kv.Value
		tag, _ := s.LoadUInt(8)
		if magic, _ := s.LoadUInt(32); magic != 0x8e81278a {
			return nil, fmt.Errorf("validator %d: bad key magic", idx)
		}
		key, err := s.LoadSlice(256)
		if err != nil {
			return nil, err
		}
		w, err := s.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		r := validatorRow{Param: param, UTimeSince: since, UTimeUntil: until, Idx: uint16(idx),
			PubKey: hex.EncodeToString(key), Weight: w, Main: uint16(idx) < main}
		if tag == 0x73 {
			if adnl, err := s.LoadSlice(256); err == nil {
				r.ADNL = hex.EncodeToString(adnl)
			}
		}
		total += w
		rows = append(rows, r)
	}
	for i := range rows {
		rows[i].Share = float64(rows[i].Weight) / float64(total)
	}
	return rows, nil
}

// watchValidators stores the current and next validator set each time one
// changes (a round is ~18 h); ReplacingMergeTree drops the repeats.
func watchValidators(ctx context.Context, api ton.APIClientWrapped) {
	var last string
	for {
		if err := func() error {
			mc, err := api.CurrentMasterchainInfo(ctx)
			if err != nil {
				return err
			}
			var rows []validatorRow
			for _, p := range []uint8{34, 36} {
				// one request per param: asking for an absent one fails the whole request
				cfg, err := api.GetBlockchainConfig(ctx, mc, int32(p))
				if p == 36 && err != nil && strings.Contains(err.Error(), "not found") {
					continue // 36 exists only between an election and the switch
				}
				if err != nil {
					return err
				}
				c := cfg.Get(int32(p))
				if c == nil {
					continue
				}
				vs, err := validatorSet(c, p)
				if err != nil {
					return fmt.Errorf("param %d: %w", p, err)
				}
				rows = append(rows, vs...)
			}
			key := fmt.Sprint(len(rows))
			if len(rows) > 0 {
				key += fmt.Sprint(rows[0].UTimeSince, rows[len(rows)-1].UTimeSince)
			}
			if key == last {
				return nil
			}
			if err := insert("validators", rows); err != nil {
				return err
			}
			last = key
			log.Printf("validator sets: %d rows", len(rows))
			return nil
		}(); err != nil {
			log.Printf("validators: %v", err)
		}
		time.Sleep(validatorEvery)
	}
}
