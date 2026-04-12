package types

import "time"

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
