// Package pricing values assets in USD at a point in time using DefiLlama's
// historical price API, addressed by the same source-backed identity as the
// asset (contract/mint), never by symbol.
package pricing

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/netcache"
)

const (
	// A quote further than this from the requested instant is not a price
	// for that instant.
	maxSkew = 2 * time.Hour
	// DefiLlama's own confidence score; low scores come from thin DEX
	// liquidity, which is exactly where spam tokens get fake prices.
	minConfidence = 0.9
)

type Pricer struct {
	Net *netcache.Client
	// Failures records why lookups failed, for the review list.
	Failures map[string]string
}

func New(net *netcache.Client) *Pricer {
	return &Pricer{Net: net, Failures: map[string]string{}}
}

// coin maps an asset key to a DefiLlama coin id; "" means unpriceable.
func coin(key string) string {
	switch key {
	case "ETH":
		return "coingecko:ethereum"
	case "SOL":
		return "coingecko:solana"
	case "BTC":
		return "coingecko:bitcoin"
	}
	chain, addr, ok := strings.Cut(key, ":")
	if !ok || strings.Contains(addr, "#") {
		return "" // NFTs have no fungible price
	}
	switch chain {
	case "ethereum", "arbitrum", "solana":
		return key
	}
	return ""
}

// Unit returns the USD price of one unit of the asset at t.
func (p *Pricer) Unit(key string, t time.Time) (*big.Rat, bool) {
	if asset.IsCash(key) {
		return big.NewRat(1, 1), true
	}
	id := coin(key)
	if id == "" {
		return nil, false
	}
	price, err := p.lookup(id, t)
	if err != nil {
		p.Failures[key] = err.Error()
		return nil, false
	}
	return price, true
}

// Value returns the USD value of |amount| units at t.
func (p *Pricer) Value(key string, amount *big.Rat, t time.Time) (*big.Rat, bool) {
	unit, ok := p.Unit(key, t)
	if !ok {
		return nil, false
	}
	v := new(big.Rat).Abs(amount)
	return v.Mul(v, unit), true
}

func (p *Pricer) lookup(id string, t time.Time) (*big.Rat, error) {
	ts := t.Unix()
	b, err := p.Net.Fetch(netcache.Request{
		Key:       fmt.Sprintf("price/%s/%d", id, ts),
		URL:       fmt.Sprintf("https://coins.llama.fi/prices/historical/%d/%s?searchWidth=2h", ts, id),
		Immutable: true,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Coins map[string]struct {
			Price      json.Number `json:"price"`
			Timestamp  int64       `json:"timestamp"`
			Confidence *float64    `json:"confidence"`
		} `json:"coins"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("defillama response: %w", err)
	}
	q, ok := resp.Coins[id]
	if !ok {
		return nil, fmt.Errorf("defillama has no price for %s at %s", id, t.UTC().Format(time.RFC3339))
	}
	if skew := time.Duration(abs(q.Timestamp-ts)) * time.Second; skew > maxSkew {
		return nil, fmt.Errorf("defillama price for %s is %s away from %s", id, skew, t.UTC().Format(time.RFC3339))
	}
	if q.Confidence != nil && *q.Confidence < minConfidence {
		return nil, fmt.Errorf("defillama confidence %.2f for %s is below %.2f", *q.Confidence, id, minConfidence)
	}
	price, err := decimal.Parse(q.Price.String())
	if err != nil || price.Sign() <= 0 {
		return nil, fmt.Errorf("defillama price %q for %s is not positive", q.Price, id)
	}
	return price, nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
