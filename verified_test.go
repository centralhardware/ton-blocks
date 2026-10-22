package main

import (
	"context"
	"os"
	"testing"
)

// LIVE=1 CODE=<base64 hash> go test -run Sources -v
func TestSourcesLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("LIVE not set")
	}
	ctx := context.Background()
	api, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	api = api.WithRetry(5)
	mc, err := settledMC(ctx, api)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{os.Getenv("CODE"), "IINLe3KxEhR+Gy+0V7hOdNGjDwT3N9T2KmaOlVLSty8="} {
		if h == "" {
			continue
		}
		link, v, err := sourceLink(ctx, api, mc, h)
		t.Logf("%s: %q %q %v", h, link, v, err)
		if link != "" {
			r := sourceRow{}
			err := fetchSources(&r, link)
			t.Logf("  %s %s %v files=%v err=%v", r.Compiler, r.Version, len(r.FileBodies), r.FileNames, err)
		}
	}
}
