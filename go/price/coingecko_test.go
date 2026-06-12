package price

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLookupRetriesOnceAfterRateLimit(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()

		if call == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"market_data": map[string]any{
				"current_price": map[string]any{"usd": 123.45},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	p := testProvider(server)
	price, err := p.Lookup("ETH", "", time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if price != "123.45" {
		t.Fatalf("expected exact decimal price 123.45, got %q", price)
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests, got %d", calls)
	}
}

func TestLookupStopsAfterRateLimitRetriesExhausted(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := testProvider(server)
	_, err := p.Lookup("ETH", "", time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited after 1 retry attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests, got %d", calls)
	}
}

func testProvider(server *httptest.Server) *Provider {
	p := NewProvider()
	p.Client = server.Client()
	p.BaseURL = server.URL
	p.Sleep = func(time.Duration) {}
	p.RateLimitDelay = 0
	p.RetryDelay = 0
	p.MaxRetries = 1
	return p
}

// A 200 response without market_data means "no price known" — it must be an
// error, never a cached $0.
func TestLookupMissingMarketDataIsError(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"ethereum","symbol":"eth"}`))
	}))
	defer server.Close()

	p := testProvider(server)
	ts := time.Date(2014, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := p.Lookup("ETH", "", ts); err == nil {
		t.Fatal("expected error for missing market_data, got nil")
	}
	// The failure is cached: a second lookup must not re-hit the API.
	if _, err := p.Lookup("ETH", "", ts); err == nil {
		t.Fatal("expected cached error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 request (failure cached), got %d", calls)
	}
}

// Sub-micro prices must survive exactly — the old float64 + %.6f path
// silently turned them into $0.
func TestLookupPreservesSubMicroPrices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"market_data":{"current_price":{"usd":1.7e-8}}}`))
	}))
	defer server.Close()

	p := testProvider(server)
	price, err := p.Lookup("DOGE", "", time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if price != "0.000000017" {
		t.Fatalf("expected exact 0.000000017, got %q", price)
	}
}

// Canonical identities decide the pricing identity; metadata symbols from
// unverified mints/contracts must not resolve (no $1 for fake stables).
func TestLookupCanonicalGating(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP call expected for these lookups")
	}))
	defer server.Close()

	p := testProvider(server)
	ts := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	// Verified USDC mint → $1 regardless of claimed symbol.
	price, err := p.Lookup("WeirdLabel", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", ts)
	if err != nil || price != "1.00" {
		t.Fatalf("verified USDC mint: price=%q err=%v, want 1.00", price, err)
	}

	// Verified EVM USDC contract, checksum-cased → still resolves.
	price, err = p.Lookup("USDC", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", ts)
	if err != nil || price != "1.00" {
		t.Fatalf("verified EVM USDC: price=%q err=%v, want 1.00", price, err)
	}

	// Unverified mint claiming USDC → error, not $1.
	if _, err := p.Lookup("USDC", "FakeMintAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ts); err == nil {
		t.Fatal("expected error for unverified canonical claiming USDC")
	}

	// No canonical (CEX/native context) → symbol trusted.
	price, err = p.Lookup("USDC", "", ts)
	if err != nil || price != "1.00" {
		t.Fatalf("plain USDC: price=%q err=%v, want 1.00", price, err)
	}
}
