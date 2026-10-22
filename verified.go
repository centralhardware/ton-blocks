package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton"
)

// Verified source: when a contract's author verified it on verifier.ton.org,
// the on-chain sources registry holds a source item for (verifier, code hash)
// whose content points to a JSON on IPFS listing the source files. This is the
// real source, unlike a decompilation; see ton-community/contract-verifier-sdk.
const (
	sourcesRegistry = "EQD-BJSVUJviud_Qv7Ymfd3qzXdrmV525e3YDzWQoHIAiInL"
	sourceEvery     = 10 * time.Minute
	sourceRetry     = 7 * 24 * 3600 // seconds before asking about an unverified hash again
	sourceRound     = 200           // code hashes per round
)

var verifiers = []string{"orbs.com", "verifier.ton.org"}

// ipfsGateways are tried in order. The verifier's own (files.orbs.network),
// ipfs.io and dweb.link all answered 429 to every request from this host on
// 2026-10-22, while Pinata served the same files.
var ipfsGateways = []string{"https://gateway.pinata.cloud/ipfs/", "https://files.orbs.network/ipfs/", "https://ipfs.io/ipfs/"}

type sourceFile struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	Entrypoint bool   `json:"entrypoint"`
}

type sourceRow struct {
	CodeHash   string   `json:"code_hash"`
	Verified   bool     `json:"verified"`
	Verifier   string   `json:"verifier"`
	Compiler   string   `json:"compiler"`
	Version    string   `json:"compiler_version"`
	Command    string   `json:"command_line"`
	FileNames  []string `json:"files.name"`
	FileBodies []string `json:"files.content"`
	FileEntry  []bool   `json:"files.entrypoint"`
	IPFS       string   `json:"ipfs"`
	CheckedAt  uint32   `json:"checked_at"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// ipfsGet fetches an ipfs:// link through the first gateway that serves it,
// about one request a second, backing off on 429.
func ipfsGet(link string) ([]byte, error) {
	var last error
	for _, gw := range ipfsGateways {
		url := strings.Replace(link, "ipfs://", gw, 1)
		for attempt := 0; attempt < 3; attempt++ {
			time.Sleep(time.Second << attempt)
			resp, err := httpClient.Get(url)
			if err != nil {
				last = err
				break
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil && resp.StatusCode == http.StatusOK {
				return b, nil
			}
			last = fmt.Errorf("%s: %s", url, resp.Status)
			if resp.StatusCode != http.StatusTooManyRequests {
				break
			}
		}
	}
	return nil, last
}

// sourceLink returns the IPFS link of the verified sources of a code hash
// (base64) and the verifier that holds them, or "" if none does.
func sourceLink(ctx context.Context, api ton.APIClientWrapped, mc *ton.BlockIDExt, codeHash string) (string, string, error) {
	hash, err := base64.StdEncoding.DecodeString(codeHash)
	if err != nil {
		return "", "", err
	}
	registry := address.MustParseAddr(sourcesRegistry)
	for _, v := range verifiers {
		vid := sha256.Sum256([]byte(v))
		res, err := api.RunGetMethod(ctx, mc, registry, "get_source_item_address",
			new(big.Int).SetBytes(vid[:]), new(big.Int).SetBytes(hash))
		if err != nil {
			return "", "", err
		}
		s, err := res.Slice(0)
		if err != nil {
			return "", "", err
		}
		item, err := s.LoadAddr()
		if err != nil {
			return "", "", err
		}
		data, err := api.RunGetMethod(ctx, mc, item, "get_source_item_data")
		if err != nil {
			// an item that was never deployed has no code to run: not verified here
			if strings.Contains(err.Error(), "-256") || strings.Contains(err.Error(), "not active") {
				continue
			}
			return "", "", err
		}
		c, err := data.Cell(3)
		if err != nil {
			return "", "", err
		}
		cs := c.MustBeginParse()
		if ver, _ := cs.LoadUInt(8); ver != 1 {
			return "", "", fmt.Errorf("source item content version %d", ver)
		}
		link, err := cs.LoadStringSnake()
		if err != nil {
			return "", "", err
		}
		return link, v, nil
	}
	return "", "", nil
}

// fetchSources downloads the sources JSON and every file it lists.
func fetchSources(r *sourceRow, link string) error {
	var meta struct {
		Compiler      string `json:"compiler"`
		KnownSettings struct {
			FuncVersion string `json:"funcVersion"`
			FiftVersion string `json:"fiftVersion"`
			TactVersion string `json:"tactVersion"`
			TolkVersion string `json:"tolkVersion"`
			CommandLine string `json:"commandLine"`
		} `json:"compilerSettings"`
		Sources []struct {
			URL        string `json:"url"`
			Filename   string `json:"filename"`
			Entrypoint bool   `json:"isEntrypoint"`
		} `json:"sources"`
	}
	r.IPFS = link
	b, err := ipfsGet(link)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return err
	}
	s := meta.KnownSettings
	r.Compiler, r.Command = meta.Compiler, s.CommandLine
	r.Version = s.FuncVersion + s.FiftVersion + s.TactVersion + s.TolkVersion
	for _, src := range meta.Sources {
		body, err := ipfsGet(src.URL)
		if err != nil {
			return err
		}
		r.FileNames = append(r.FileNames, src.Filename)
		r.FileBodies = append(r.FileBodies, string(body))
		r.FileEntry = append(r.FileEntry, src.Entrypoint)
	}
	return nil
}

// settledMC is an mc block a minute behind the tip: get-methods at the tip
// fail on liteservers whose shard state lags ("block handle not in db").
func settledMC(ctx context.Context, api ton.APIClientWrapped) (*ton.BlockIDExt, error) {
	tip, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}
	return api.LookupBlock(ctx, tip.Workchain, tip.Shard, tip.SeqNo-30)
}

// uncheckedCode: code hashes with no source row, or unverified ones due again.
func uncheckedCode() ([]string, error) {
	out, err := post(fmt.Sprintf("SELECT code_hash FROM %[1]s.code WHERE code_hash NOT IN "+
		"(SELECT code_hash FROM %[1]s.code_sources FINAL WHERE verified OR checked_at > now() - %[2]d) "+
		"LIMIT %[3]d FORMAT TSV", database, sourceRetry, sourceRound), nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// resolveSources runs forever next to the follower: one INSERT per round.
func resolveSources(ctx context.Context, api ton.APIClientWrapped) {
	api = api.WithRetry(5) // single liteservers lag behind now and then
	for {
		if err := func() error {
			hashes, err := uncheckedCode()
			if err != nil || len(hashes) == 0 {
				return err
			}
			mc, err := settledMC(ctx, api)
			if err != nil {
				return err
			}
			var rows []sourceRow
			found := 0
			for _, h := range hashes {
				r := sourceRow{CodeHash: h, CheckedAt: uint32(time.Now().Unix()),
					FileNames: []string{}, FileBodies: []string{}, FileEntry: []bool{}}
				link, verifier, err := sourceLink(ctx, api, mc, h)
				if err != nil {
					log.Printf("sources %s: %v", h, err)
					continue // asked again next round
				}
				if link != "" {
					if err := fetchSources(&r, link); err != nil {
						log.Printf("sources %s: %v", h, err)
						continue
					}
					r.Verified, r.Verifier = true, verifier
					found++
				}
				rows = append(rows, r)
			}
			if found > 0 {
				log.Printf("verified sources: %d of %d codes", found, len(rows))
			}
			return insert("code_sources", rows)
		}(); err != nil {
			log.Printf("sources: %v", err)
		}
		time.Sleep(sourceEvery)
	}
}
