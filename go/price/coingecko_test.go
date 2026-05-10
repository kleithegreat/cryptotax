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
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	p := testProvider(server)
	price, err := p.Lookup("ETH", time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if price != "123.450000" {
		t.Fatalf("expected price 123.450000, got %q", price)
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
	_, err := p.Lookup("ETH", time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC))
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
