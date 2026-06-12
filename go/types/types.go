package types

import (
	"sort"
	"strings"
	"time"
)

// TxPayload is the top-level JSON object sent to the Haskell core via stdin.
type TxPayload struct {
	Version      string        `json:"version"`
	Wallets      []string      `json:"wallets"`
	Transactions []Transaction `json:"transactions"`
}

// TxType classifies each transaction for tax purposes.
type TxType string

const (
	TxBuy            TxType = "buy"
	TxSell           TxType = "sell"
	TxSwap           TxType = "swap"
	TxTransferIn     TxType = "transfer_in"
	TxTransferOut    TxType = "transfer_out"
	TxIncome         TxType = "income"
	TxFundingPayment TxType = "funding_payment"
	TxPerpOpen       TxType = "perp_open"
	TxPerpClose      TxType = "perp_close"
)

// Source identifies which fetcher produced the transaction.
type Source string

const (
	SourceEtherscan   Source = "etherscan"
	SourceHelius      Source = "helius"
	SourceHyperliquid Source = "hyperliquid"
	SourceRobinhood   Source = "robinhood"
)

// Chain identifies the chain or platform.
type Chain string

const (
	ChainEthereum    Chain = "ethereum"
	ChainArbitrum    Chain = "arbitrum"
	ChainSolana      Chain = "solana"
	ChainHyperliquid Chain = "hyperliquid"
	ChainRobinhood   Chain = "robinhood"
)

// Transaction is a single normalized transaction, source-agnostic.
type Transaction struct {
	ID           string       `json:"id"`
	Timestamp    time.Time    `json:"timestamp"`
	Source       Source       `json:"source"`
	Chain        Chain        `json:"chain"`
	TxType       TxType       `json:"tx_type"`
	Wallet       string       `json:"wallet"`
	Counterparty *string      `json:"counterparty"`
	Sent         *AssetAmount `json:"sent"`
	Received     *AssetAmount `json:"received"`
	Fee          *AssetAmount `json:"fee"`
	RawType      *string      `json:"raw_type"`
	Market       *string      `json:"market,omitempty"`
	EventGroupID *string      `json:"event_group_id,omitempty"`
	SplitReason  *string      `json:"split_reason,omitempty"`
	ClosedPnl    *string      `json:"closed_pnl,omitempty"`
}

// AssetAmount represents a quantity of a token with its USD valuation.
// Amount and USDValue are strings to preserve exact decimal precision.
// The Haskell core parses these into exact rationals — no floats anywhere.
type AssetAmount struct {
	Asset          string  `json:"asset"`
	AssetCanonical *string `json:"asset_canonical,omitempty"`
	Amount         string  `json:"amount"`
	USDValue       string  `json:"usd_value"`
}

// CanonicalWallet normalizes a wallet identifier for identity comparison.
// EVM addresses are case-insensitive (checksum casing is display-only), so
// 0x-hex addresses fold to lowercase. Solana addresses are case-sensitive
// base58 and must pass through unchanged.
func CanonicalWallet(wallet string) string {
	trimmed := strings.TrimSpace(wallet)
	if isHexAddress(trimmed) {
		return strings.ToLower(trimmed)
	}
	return trimmed
}

func isHexAddress(s string) bool {
	if len(s) != 42 || s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
		return false
	}
	for _, r := range s[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// SortTransactions orders the payload deterministically: by timestamp, then
// acquisitions before disposals (sources emit second-truncated timestamps,
// and a same-second buy+sell would otherwise race — FIFO needs the lot to
// exist before the sale consumes it), then by ID and wallet as final
// tiebreakers. The sort is stable, so equal keys keep input order.
func SortTransactions(txs []Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		a, b := txs[i], txs[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		if ra, rb := txTypeRank(a.TxType), txTypeRank(b.TxType); ra != rb {
			return ra < rb
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Wallet < b.Wallet
	})
}

// txTypeRank orders same-timestamp rows: pure acquisitions first, mixed
// (swap) in the middle, pure disposals last.
func txTypeRank(t TxType) int {
	switch t {
	case TxBuy, TxIncome, TxFundingPayment, TxTransferIn, TxPerpOpen:
		return 0
	case TxSwap, TxPerpClose:
		return 1
	case TxSell, TxTransferOut:
		return 2
	default:
		return 3
	}
}
