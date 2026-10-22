package main

import (
	"context"
	"os"
	"testing"
)

// LIVE=1 go test -run Validators: reads the real validator set from mainnet.
func TestValidatorsLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("LIVE not set")
	}
	ctx := context.Background()
	api, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mc, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := api.GetBlockchainConfig(ctx, mc, 34)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := validatorSet(cfg.Get(34), 34)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 100 {
		t.Fatalf("only %d validators", len(rows))
	}
	t.Logf("%d validators, first %+v", len(rows), rows[0])
}

func TestStateLookupsLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("LIVE not set")
	}
	bals, err := lookupBalances([]string{"0:0007ea6f21153e666e6cffd956ddb704a12119bfe84827d5ff9523ecc18767d8"})
	if err != nil || len(bals) != 1 {
		t.Fatalf("balances: %v %v", bals, err)
	}
	nfts, cols, err := lookupNFTs([]string{"0:16ce605d4c6b88e7ae7f1920d26556129ddcddce6c0f914f0f886fa3e5c83982"})
	if err != nil || len(nfts) != 1 || nfts[0].Name == "" {
		t.Fatalf("nfts: %+v %v", nfts, err)
	}
	t.Logf("%+v\n%+v\n%+v", bals[0], nfts[0], cols)
}
