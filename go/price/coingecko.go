package price

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Provider resolves historical USD prices for crypto assets.
// Uses CoinGecko's free API: 30 calls/min, 10k calls/month.
type Provider struct {
	Client         *http.Client
	BaseURL        string
	Sleep          func(time.Duration)
	RateLimitDelay time.Duration
	RetryDelay     time.Duration
	MaxRetries     int
	cache          map[string]string // "ASSET:2024-03-15" -> "3456.78"
	mu             sync.Mutex
}

func NewProvider() *Provider {
	return &Provider{
		Client:         &http.Client{Timeout: 15 * time.Second},
		BaseURL:        "https://api.coingecko.com/api/v3",
		Sleep:          time.Sleep,
		RateLimitDelay: 2 * time.Second,
		RetryDelay:     60 * time.Second,
		MaxRetries:     1,
		cache:          make(map[string]string),
	}
}

// coingeckoIDs maps token symbols to CoinGecko API identifiers.
// Extend this as you add support for more tokens.
var coingeckoIDs = map[string]string{
	"ETH":  "ethereum",
	"BTC":  "bitcoin",
	"SOL":  "solana",
	"USDC": "usd-coin",
	"USDT": "tether",
	"ARB":  "arbitrum",
	"SUI":  "sui",
	"DOGE": "dogecoin",
	"AVAX": "avalanche-2",
	"LINK": "chainlink",
	"UNI":  "uniswap",
}

// Lookup returns the USD price of an asset at a given timestamp.
// Stablecoins short-circuit to "1.00".
// Results are cached to minimize API calls.
func (p *Provider) Lookup(asset string, ts time.Time) (string, error) {
	upper := strings.ToUpper(asset)

	// Stablecoins don't need a lookup
	if upper == "USDC" || upper == "USDT" || upper == "DAI" || upper == "BUSD" {
		return "1.00", nil
	}

	dateKey := ts.UTC().Format("02-01-2006") // DD-MM-YYYY for CoinGecko
	cacheKey := fmt.Sprintf("%s:%s", upper, dateKey)

	p.mu.Lock()
	if cached, ok := p.cache[cacheKey]; ok {
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	cgID, ok := coingeckoIDs[upper]
	if !ok {
		return "", fmt.Errorf("unknown asset %q — add it to coingeckoIDs", upper)
	}

	// CoinGecko free API: /coins/{id}/history?date=DD-MM-YYYY
	url := fmt.Sprintf(
		"%s/coins/%s/history?date=%s&localization=false",
		strings.TrimRight(p.baseURL(), "/"), cgID, dateKey,
	)

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		// Rate limit: 30 calls/min = 1 every 2 seconds to be safe.
		p.sleep(p.RateLimitDelay)

		var err error
		resp, err = p.Client.Get(url)
		if err != nil {
			return "", fmt.Errorf("CoinGecko request failed: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			break
		}

		_ = resp.Body.Close()
		if attempt >= p.MaxRetries {
			return "", fmt.Errorf("CoinGecko rate limited after %d retry attempts", attempt)
		}
		p.sleep(p.RetryDelay)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		MarketData struct {
			CurrentPrice struct {
				USD float64 `json:"usd"`
			} `json:"current_price"`
		} `json:"market_data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing CoinGecko response: %w", err)
	}

	priceStr := fmt.Sprintf("%.6f", result.MarketData.CurrentPrice.USD)

	p.mu.Lock()
	p.cache[cacheKey] = priceStr
	p.mu.Unlock()

	return priceStr, nil
}

func (p *Provider) sleep(delay time.Duration) {
	if delay <= 0 || p.Sleep == nil {
		return
	}
	p.Sleep(delay)
}

func (p *Provider) baseURL() string {
	if p.BaseURL == "" {
		return "https://api.coingecko.com/api/v3"
	}
	return p.BaseURL
}
