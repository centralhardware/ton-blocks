package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// toncenter's free tier allows 1 request a second per IP; the jetton and the
// contract lookups share it through toncenterGet.
const toncenterPause = 1100 * time.Millisecond

var (
	toncenterMu   sync.Mutex
	toncenterLast time.Time
)

// toncenterGet calls an /api/v3 endpoint, waiting its turn, and decodes the JSON.
// TONCENTER_API_KEY, when set, lifts the rate limit. A 429 (the limit is per IP,
// so anything else on the host counts too) is retried after a pause.
func toncenterGet(path string, q url.Values, out any) error {
	for attempt := 1; ; attempt++ {
		err := toncenterOnce(path, q, out)
		if !errors.Is(err, errRateLimited) || attempt == 4 {
			return err
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
}

var errRateLimited = errors.New("toncenter: rate limited")

func toncenterOnce(path string, q url.Values, out any) error {
	toncenterMu.Lock()
	if wait := toncenterPause - time.Since(toncenterLast); wait > 0 {
		time.Sleep(wait)
	}
	toncenterLast = time.Now()
	toncenterMu.Unlock()

	req, _ := http.NewRequest("GET", "https://toncenter.com/api/v3/"+path+"?"+q.Encode(), nil)
	if key := os.Getenv("TONCENTER_API_KEY"); key != "" {
		req.Header.Set("X-API-Key", key)
	}
	client := http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("toncenter %s: %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
