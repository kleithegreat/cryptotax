// Package netcache performs source API requests with retry and keeps every
// response on disk. The cache is the evidence trail: an --offline run
// reproduces a report exactly from what the sources said.
package netcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Dir     string
	Offline bool
	HTTP    *http.Client
	// Gap is the minimum spacing between requests to one host.
	Gap map[string]time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

func New(dir string, offline bool) *Client {
	return &Client{
		Dir:     dir,
		Offline: offline,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Gap: map[string]time.Duration{
			"api.etherscan.io":       350 * time.Millisecond,
			"api.hyperliquid.xyz":    200 * time.Millisecond,
			"mainnet.helius-rpc.com": 120 * time.Millisecond,
			"coins.llama.fi":         150 * time.Millisecond,
		},
		last: map[string]time.Time{},
	}
}

// Request describes one call. Key names the cache entry and must not contain
// secrets; URL may (it is never logged or stored).
type Request struct {
	Key       string
	URL       string
	Body      []byte // POST when non-nil
	Immutable bool   // safe to serve from cache without refetching
	// Retry reports a 200 response that is really a transient refusal
	// (some APIs signal rate limits in the body); it is retried, never cached.
	Retry func(body []byte) bool
}

var ErrOffline = errors.New("not in cache and running offline")

func (c *Client) Fetch(r Request) ([]byte, error) {
	path := filepath.Join(c.Dir, cacheName(r.Key))
	if r.Immutable || c.Offline {
		if b, err := os.ReadFile(path); err == nil {
			return b, nil
		}
		if c.Offline {
			return nil, fmt.Errorf("%s: %w", r.Key, ErrOffline)
		}
	}
	b, err := c.do(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.Key, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Client) do(r Request) ([]byte, error) {
	host := hostOf(r.URL)
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		c.pace(host)
		method := http.MethodGet
		var body io.Reader
		if r.Body != nil {
			method, body = http.MethodPost, bytes.NewReader(r.Body)
		}
		req, err := http.NewRequest(method, r.URL, body)
		if err != nil {
			return nil, errors.New("bad request URL")
		}
		if r.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			// url.Error embeds the URL, which may carry an API key.
			lastErr = fmt.Errorf("request to %s failed", host)
			time.Sleep(backoff(attempt))
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s: HTTP %d", host, resp.StatusCode)
			time.Sleep(backoff(attempt))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: HTTP %d: %s", host, resp.StatusCode, snippet(b))
		}
		if r.Retry != nil && r.Retry(b) {
			lastErr = fmt.Errorf("%s: %s", host, snippet(b))
			time.Sleep(backoff(attempt))
			continue
		}
		return b, nil
	}
	return nil, lastErr
}

func (c *Client) pace(host string) {
	gap := c.Gap[host]
	c.mu.Lock()
	wait := time.Until(c.last[host].Add(gap))
	if wait > 0 {
		c.last[host] = c.last[host].Add(gap)
	} else {
		c.last[host] = time.Now()
	}
	c.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<attempt) * time.Second
}

func hostOf(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// cacheName keeps keys readable while staying filesystem-safe.
func cacheName(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if len(name) > 180 {
		sum := sha256.Sum256([]byte(key))
		name = name[:120] + "-" + hex.EncodeToString(sum[:8])
	}
	return name + ".json"
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
