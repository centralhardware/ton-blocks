package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

// Which code an account runs is chain state, not in the blocks (a transaction
// carries only state hashes). resolveContracts asks toncenter's index for the
// accounts seen in recent transactions and stores code hash and interfaces in
// ton.contracts, so the board can break activity down by contract type.
const (
	contractBatch    = 100  // addresses per toncenter request
	contractQuery    = 2000 // accounts fetched from ClickHouse per round trip
	contractEvery    = time.Minute
	contractLookback = 7 * 24 * 3600 // seconds of transactions to look for new accounts in
	contractRetry    = 6 * 3600      // seconds before asking about an account without code again
)

type contract struct {
	Account    string   `json:"account"`
	Status     string   `json:"status"`
	CodeHash   string   `json:"code_hash"`
	Interfaces []string `json:"interfaces"`
}

func unresolvedAccounts() ([]string, error) {
	out, err := post(fmt.Sprintf("SELECT DISTINCT account FROM %[1]s.transactions "+
		"WHERE now > now() - %[2]d AND account NOT IN "+
		"(SELECT account FROM %[1]s.contracts FINAL WHERE code_hash != '' OR resolved_at > now() - %[3]d) "+
		"LIMIT %[4]d FORMAT TSV", database, contractLookback, contractRetry, contractQuery), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func lookupContracts(accounts []string) ([]contract, error) {
	var body struct {
		Accounts []struct {
			Address, Status string
			CodeHash        string   `json:"code_hash"`
			Interfaces      []string `json:"interfaces"`
		} `json:"accounts"`
	}
	q := url.Values{"address": accounts, "include_boc": {"false"}}
	if err := toncenterGet("accountStates", q, &body); err != nil {
		return nil, err
	}
	found := map[string]contract{}
	for _, a := range body.Accounts {
		c := contract{strings.ToLower(a.Address), a.Status, a.CodeHash, a.Interfaces}
		if c.Interfaces == nil {
			c.Interfaces = []string{}
		}
		found[c.Account] = c
	}
	res := make([]contract, 0, len(accounts))
	for _, a := range accounts {
		if c, ok := found[a]; ok {
			res = append(res, c)
		} else {
			res = append(res, contract{Account: a, Interfaces: []string{}})
		}
	}
	return res, nil
}

// resolveContracts runs forever next to the follower, like resolveJettons.
func resolveContracts() {
	for {
		total := 0
		for {
			accounts, err := unresolvedAccounts()
			for i := 0; err == nil && i < len(accounts); i += contractBatch {
				var cs []contract
				if cs, err = lookupContracts(accounts[i:min(i+contractBatch, len(accounts))]); err == nil {
					err = insert("contracts", cs)
					total += len(cs)
				}
			}
			if err != nil {
				log.Printf("contract lookup: %v", err)
				break
			}
			if len(accounts) < contractQuery {
				break
			}
		}
		if total >= 10000 {
			log.Printf("contract lookup: %d accounts", total)
		}
		time.Sleep(contractEvery)
	}
}
