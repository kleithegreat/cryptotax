package price

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kevin/cryptotax/decimal"
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
	cache          map[string]string // "ASSET:15-03-2024" -> "3456.78"
	failed         map[string]error  // negative cache: avoid re-hitting known failures
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
		failed:         make(map[string]error),
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

// canonicalSymbols maps verified on-chain identities (lowercased EVM contract
// addresses, Solana mints) to the symbol they are allowed to price as. Token
// metadata is attacker-controlled: any scam token can call itself "USDC" and
// would otherwise be priced at $1. When a canonical identity accompanies a
// lookup, it — not the claimed symbol — decides the price identity.
var canonicalSymbols = map[string]string{
	// Solana mints
	"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v": "USDC",
	"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB": "USDT",
	"So11111111111111111111111111111111111111112":  "SOL",
	// Ethereum mainnet
	"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": "USDC",
	"0xdac17f958d2ee523a2206206994597c13d831ec7": "USDT",
	"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2": "ETH", // WETH
	// Arbitrum
	"0xaf88d065e77c8cc2239327c5edb3a432268e5831": "USDC",
	"0xff970a61a04b1ca14834a43f5de4533ebddb5cc8": "USDC", // USDC.e (bridged)
	"0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9": "USDT",
	"0x82af49447d8a07e3bd95bd0d56f35241523fbab1": "ETH", // WETH
	"0x912ce59144191c1204e64559fe8253a0e49e6548": "ARB",
}

// Lookup returns the USD price of an asset at a given timestamp.
//
// canonical, when non-empty, is the asset's source-backed on-chain identity
// (contract address or mint) and takes precedence over the display symbol:
// unknown canonicals fail the lookup instead of trusting metadata symbols.
// An empty canonical means the asset has no separate on-chain identity
// (native coins, CEX rows) and the symbol is trusted as-is.
//
// Stablecoins short-circuit to "1.00". Results (and failures) are cached to
// minimize API calls. Prices stay in exact decimal strings end to end —
// CoinGecko's JSON number is captured as json.Number, never float64.
func (p *Provider) Lookup(asset, canonical string, ts time.Time) (string, error) {
	upper := strings.ToUpper(asset)

	if canonical != "" {
		sym, ok := canonicalSymbols[canonicalKey(canonical)]
		if !ok {
			return "", fmt.Errorf("unverified asset identity %q (symbol %q) — add it to price.canonicalSymbols if legitimate", canonical, asset)
		}
		upper = sym
	}

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
	if failure, ok := p.failed[cacheKey]; ok {
		p.mu.Unlock()
		return "", failure
	}
	p.mu.Unlock()

	price, err := p.fetchPrice(upper, dateKey)

	p.mu.Lock()
	if err != nil {
		p.failed[cacheKey] = err
	} else {
		p.cache[cacheKey] = price
	}
	p.mu.Unlock()

	return price, err
}

func (p *Provider) fetchPrice(symbol, dateKey string) (string, error) {
	cgID, ok := coingeckoIDs[symbol]
	if !ok {
		return "", fmt.Errorf("unknown asset %q — add it to coingeckoIDs", symbol)
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

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CoinGecko HTTP %d for %s on %s", resp.StatusCode, cgID, dateKey)
	}

	var result struct {
		MarketData *struct {
			CurrentPrice struct {
				USD json.Number `json:"usd"`
			} `json:"current_price"`
		} `json:"market_data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing CoinGecko response: %w", err)
	}

	// Dates before a coin's listing return 200 with no market_data; that is
	// "no price known", not $0.
	if result.MarketData == nil || result.MarketData.CurrentPrice.USD.String() == "" {
		return "", fmt.Errorf("CoinGecko has no market data for %s on %s", cgID, dateKey)
	}

	price, err := decimal.Canon(result.MarketData.CurrentPrice.USD.String())
	if err != nil {
		return "", fmt.Errorf("CoinGecko price for %s on %s: %w", cgID, dateKey, err)
	}

	return price, nil
}

// canonicalKey normalizes a canonical identity for table lookup: EVM
// addresses are case-insensitive (fold), Solana mints are case-sensitive
// base58 (keep).
func canonicalKey(canonical string) string {
	if strings.HasPrefix(canonical, "0x") || strings.HasPrefix(canonical, "0X") {
		return strings.ToLower(canonical)
	}
	return canonical
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
