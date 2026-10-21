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
	codeBatch        = 20            // accounts per BOC request: their data comes along and can be large
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

type code struct {
	CodeHash string `json:"code_hash"`
	CodeBOC  string `json:"code_boc"`
	Size     int    `json:"size"`
	Example  string `json:"example"`
}

// newCode returns one example account for each code hash not yet in ton.code.
func newCode() ([]string, error) {
	out, err := post(fmt.Sprintf("SELECT any(account) FROM %[1]s.contracts FINAL "+
		"WHERE code_hash != '' AND code_hash NOT IN (SELECT code_hash FROM %[1]s.code) "+
		"GROUP BY code_hash LIMIT %[2]d FORMAT TSV", database, contractQuery), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func fetchCode(accounts []string) ([]code, error) {
	var body struct {
		Accounts []struct {
			Address  string
			CodeHash string `json:"code_hash"`
			CodeBOC  string `json:"code_boc"`
		} `json:"accounts"`
	}
	q := url.Values{"address": accounts, "include_boc": {"true"}}
	if err := toncenterGet("accountStates", q, &body); err != nil {
		return nil, err
	}
	var res []code
	for _, a := range body.Accounts {
		if a.CodeHash != "" && a.CodeBOC != "" {
			res = append(res, code{a.CodeHash, a.CodeBOC, len(a.CodeBOC) * 3 / 4, strings.ToLower(a.Address)})
		}
	}
	return res, nil
}

// resolveContracts runs forever next to the follower, like resolveJettons.
func resolveContracts() {
	for {
		total := 0
		for {
			// one INSERT per round of contractQuery accounts, not per toncenter
			// request: each insert is a part, and requests come every second
			accounts, err := unresolvedAccounts()
			var batch []contract
			for i := 0; err == nil && i < len(accounts); i += contractBatch {
				var cs []contract
				if cs, err = lookupContracts(accounts[i:min(i+contractBatch, len(accounts))]); err == nil {
					batch = append(batch, cs...)
				}
			}
			if ierr := insert("contracts", batch); ierr != nil && err == nil {
				err = ierr
			}
			total += len(batch)
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
		// then the code of hashes seen for the first time; a hash whose example
		// account changed code meanwhile is just asked again next round
		accounts, err := newCode()
		var codes []code
		for i := 0; err == nil && i < len(accounts); i += codeBatch {
			var cs []code
			if cs, err = fetchCode(accounts[i:min(i+codeBatch, len(accounts))]); err == nil {
				codes = append(codes, cs...)
			}
		}
		if ierr := insert("code", codes); ierr != nil && err == nil {
			err = ierr
		}
		if err != nil {
			log.Printf("code lookup: %v", err)
		}
		time.Sleep(contractEvery)
	}
}
