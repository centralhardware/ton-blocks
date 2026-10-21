package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Jetton transfers name only the token *wallet* that received them; the token
// itself is that wallet's master, which is chain state. resolveJettons asks
// toncenter's index for the wallets seen in recent jetton ops and stores the
// mapping in ton.jetton_wallets, plus each master's name in ton.jettons.
const (
	jettonBatch    = 100
	jettonEvery    = time.Minute
	jettonLookback = 7 * 24 * 3600 // seconds of transactions to look for new wallets in
	jettonRetry    = 6 * 3600      // seconds before asking about an unknown wallet again
)

// ops received by a jetton wallet: transfer, internal_transfer, burn
var jettonOps = []uint32{0x0f8a7ea5, 0x178d4519, 0x595f07bc}

type jettonWallet struct {
	Wallet string `json:"wallet"`
	Jetton string `json:"jetton"`
	Owner  string `json:"owner"`
}

type jetton struct {
	Jetton   string `json:"jetton"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals uint8  `json:"decimals"`
	Image    string `json:"image"`
}

func unresolvedWallets() ([]string, error) {
	ops := make([]string, len(jettonOps))
	for i, op := range jettonOps {
		ops[i] = strconv.FormatUint(uint64(op), 10)
	}
	out, err := post(fmt.Sprintf("SELECT DISTINCT account FROM %[1]s.transactions "+
		"WHERE now > now() - %[2]d AND op IN (%[3]s) AND account NOT IN "+
		"(SELECT wallet FROM %[1]s.jetton_wallets FINAL WHERE jetton != '' OR resolved_at > now() - %[4]d) "+
		"LIMIT %[5]d FORMAT TSV", database, jettonLookback, strings.Join(ops, ","), jettonRetry, jettonBatch), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func lookupJettons(wallets []string) ([]jettonWallet, []jetton, error) {
	q := url.Values{"limit": {strconv.Itoa(len(wallets))}, "address": wallets}
	var body struct {
		JettonWallets []struct {
			Address, Owner, Jetton string
		} `json:"jetton_wallets"`
		Metadata map[string]struct {
			TokenInfo []struct {
				Type, Name, Symbol, Image string
				Extra                     struct {
					Decimals json.RawMessage `json:"decimals"` // a string or a number
				}
			} `json:"token_info"`
		} `json:"metadata"`
	}
	if err := toncenterGet("jetton/wallets", q, &body); err != nil {
		return nil, nil, err
	}
	found := map[string]jettonWallet{}
	var masters []jetton
	seen := map[string]bool{}
	for _, w := range body.JettonWallets {
		jw := jettonWallet{strings.ToLower(w.Address), strings.ToLower(w.Jetton), strings.ToLower(w.Owner)}
		found[jw.Wallet] = jw
		if seen[jw.Jetton] {
			continue
		}
		seen[jw.Jetton] = true
		j := jetton{Jetton: jw.Jetton}
		for _, ti := range body.Metadata[w.Jetton].TokenInfo {
			if ti.Type == "jetton_masters" {
				d, _ := strconv.ParseUint(strings.Trim(string(ti.Extra.Decimals), `"`), 10, 8)
				j.Symbol, j.Name, j.Image, j.Decimals = ti.Symbol, ti.Name, ti.Image, uint8(d)
				break
			}
		}
		if j.Symbol == "" && j.Name == "" {
			j.Decimals = 9 // the TEP-74 default
		}
		masters = append(masters, j)
	}
	// wallets toncenter doesn't know (not a jetton wallet, or not indexed yet)
	// are stored empty, so they wait jettonRetry before the next ask
	res := make([]jettonWallet, 0, len(wallets))
	for _, w := range wallets {
		if jw, ok := found[w]; ok {
			res = append(res, jw)
		} else {
			res = append(res, jettonWallet{Wallet: w})
		}
	}
	return res, masters, nil
}

// resolveJettons runs forever next to the follower. Errors are logged and the
// batch is asked again on the next round; the follower never waits on it.
func resolveJettons() {
	for {
		total := 0
		for {
			wallets, err := unresolvedWallets()
			if err == nil && len(wallets) > 0 {
				var ws []jettonWallet
				var js []jetton
				if ws, js, err = lookupJettons(wallets); err == nil {
					// masters first: a wallet row never points at a missing name
					if err = insert("jettons", js); err == nil {
						err = insert("jetton_wallets", ws)
					}
				}
				total += len(wallets)
			}
			if err != nil {
				log.Printf("jetton lookup: %v", err)
				break
			}
			if len(wallets) < jettonBatch {
				break
			}
		}
		if total >= 1000 {
			log.Printf("jetton lookup: %d wallets", total)
		}
		time.Sleep(jettonEvery)
	}
}
