package fetcher

import "github.com/kevin/cryptotax/types"

// Fetcher is the interface every chain-specific data source implements.
// Each fetcher takes wallet addresses and returns raw transactions
// that the normalizer will convert into the unified schema.
type Fetcher interface {
	// Name returns the source identifier for logging.
	Name() string

	// Fetch retrieves all transactions for the given wallet address.
	// The returned RawTransaction slice is chain-specific; the normalizer
	// handles conversion to types.Transaction.
	Fetch(wallet string) ([]RawTransaction, error)
}

// RawTransaction holds chain-specific data before normalization.
// Each fetcher populates the fields it can; the normalizer interprets them.
type RawTransaction struct {
	ID        string
	Timestamp int64 // unix seconds
	Source    types.Source
	Chain     types.Chain
	Wallet    string // the user wallet that this source row belongs to

	// Token movements — not all fields are populated for every chain.
	FromAddr string
	ToAddr   string
	Asset    string
	// AssetSymbol is an optional source-backed display symbol for Asset.
	// For Helius token rows, Asset may be the mint while AssetSymbol remains
	// blank unless the source explicitly provides a separate symbol.
	AssetSymbol string
	Amount      string // exact decimal string
	Fee         string // exact decimal string
	FeeAsset    string

	// For swaps: the other side of the trade.
	Asset2       string
	Asset2Symbol string
	Amount2      string

	// Source-provided USD price, if available (Robinhood, Hyperliquid).
	USDPrice string

	// Original type string from the source for debugging.
	RawType string
	Market  string

	// Optional grouping metadata for cases where one source event yields multiple
	// normalized rows, or where a weak source id needs a stable event key.
	EventGroupID string
	SplitReason  string

	// Hyperliquid perp fields — populated only for fill rows.
	ClosedPnl     string // exchange-reported realized PnL for this fill
	StartPosition string // position size before this fill
}
