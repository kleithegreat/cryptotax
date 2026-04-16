package normalize

import (
	"testing"
	"time"

	"github.com/kevin/cryptotax/fetcher"
	"github.com/kevin/cryptotax/price"
	"github.com/kevin/cryptotax/types"
)

func TestNormalizeUsesExplicitWalletForOwnTransfer(t *testing.T) {
	t.Parallel()

	raws := []fetcher.RawTransaction{
		{
			ID:        "0xabc",
			Timestamp: 1700000000,
			Source:    types.SourceEtherscan,
			Chain:     types.ChainEthereum,
			Wallet:    "0xaaa",
			FromAddr:  "0xaaa",
			ToAddr:    "0xbbb",
			Asset:     "ETH",
			Amount:    "1.0",
			Fee:       "0.01",
			FeeAsset:  "ETH",
		},
		{
			ID:        "0xabc",
			Timestamp: 1700000000,
			Source:    types.SourceEtherscan,
			Chain:     types.ChainEthereum,
			Wallet:    "0xbbb",
			FromAddr:  "0xaaa",
			ToAddr:    "0xbbb",
			Asset:     "ETH",
			Amount:    "1.0",
			Fee:       "0.01",
			FeeAsset:  "ETH",
		},
	}

	txs, err := Normalize(raws, []string{"0xaaa", "0xbbb"}, nil)
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 normalized transactions, got %d", len(txs))
	}

	if txs[0].Wallet != "0xaaa" || txs[0].TxType != types.TxTransferOut {
		t.Fatalf("expected sender row to stay with wallet 0xaaa as transfer_out, got wallet=%q type=%q", txs[0].Wallet, txs[0].TxType)
	}
	if txs[0].Counterparty == nil || *txs[0].Counterparty != "0xbbb" {
		t.Fatalf("expected sender counterparty 0xbbb, got %#v", txs[0].Counterparty)
	}
	if txs[0].Fee == nil {
		t.Fatalf("expected sender row to carry fee")
	}

	if txs[1].Wallet != "0xbbb" || txs[1].TxType != types.TxTransferIn {
		t.Fatalf("expected receiver row to stay with wallet 0xbbb as transfer_in, got wallet=%q type=%q", txs[1].Wallet, txs[1].TxType)
	}
	if txs[1].Counterparty == nil || *txs[1].Counterparty != "0xaaa" {
		t.Fatalf("expected receiver counterparty 0xaaa, got %#v", txs[1].Counterparty)
	}
	if txs[1].Fee != nil {
		t.Fatalf("expected receiver row to omit duplicated fee")
	}
}

func TestNormalizeHyperliquidFundingPositiveAsReceivedUSDC(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:        "funding-positive",
		Timestamp: time.Date(2025, 12, 2, 0, 0, 0, 0, time.UTC).Unix(),
		Source:    types.SourceHyperliquid,
		Chain:     types.ChainHyperliquid,
		Wallet:    "0xwallet",
		Amount:    "1.879512",
		RawType:   "funding",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxFundingPayment {
		t.Fatalf("expected tx_type %q, got %q", types.TxFundingPayment, tx.TxType)
	}
	if tx.Sent != nil {
		t.Fatalf("expected positive funding not to populate sent leg, got %#v", tx.Sent)
	}
	if tx.Received == nil {
		t.Fatal("expected positive funding to populate received leg")
	}
	if tx.Received.Asset != "USDC" || tx.Received.Amount != "1.879512" || tx.Received.USDValue != "1.879512" {
		t.Fatalf("unexpected received leg: %#v", tx.Received)
	}
}

func TestNormalizeHyperliquidFundingNegativeAsSentUSDC(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:        "funding-negative",
		Timestamp: time.Date(2025, 10, 7, 0, 0, 0, 0, time.UTC).Unix(),
		Source:    types.SourceHyperliquid,
		Chain:     types.ChainHyperliquid,
		Wallet:    "0xwallet",
		Amount:    "-0.168095",
		RawType:   "funding",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxFundingPayment {
		t.Fatalf("expected tx_type %q, got %q", types.TxFundingPayment, tx.TxType)
	}
	if tx.Received != nil {
		t.Fatalf("expected negative funding not to populate received leg, got %#v", tx.Received)
	}
	if tx.Sent == nil {
		t.Fatal("expected negative funding to populate sent leg")
	}
	if tx.Sent.Asset != "USDC" || tx.Sent.Amount != "0.168095" || tx.Sent.USDValue != "0.168095" {
		t.Fatalf("unexpected sent leg: %#v", tx.Sent)
	}
}

func TestNormalizeHeliusPreservesMintIdentityWithoutSourceSymbol(t *testing.T) {
	t.Parallel()

	mint := "So11111111111111111111111111111111111111112"
	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:        "sol-transfer",
		Timestamp: 1700000000,
		Source:    types.SourceHelius,
		Chain:     types.ChainSolana,
		Wallet:    "wallet",
		FromAddr:  "sender",
		ToAddr:    "wallet",
		Asset:     mint,
		Amount:    "1.25",
		RawType:   "TRANSFER",
	}, map[string]bool{"wallet": true}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxTransferIn {
		t.Fatalf("expected tx_type %q, got %q", types.TxTransferIn, tx.TxType)
	}
	if tx.Received == nil {
		t.Fatal("expected received leg")
	}
	if tx.Received.Asset != mint {
		t.Fatalf("expected exact mint identity %q, got %q", mint, tx.Received.Asset)
	}
	if tx.Received.USDValue != "0" {
		t.Fatalf("expected unresolved mint usd_value 0, got %q", tx.Received.USDValue)
	}
}

func TestNormalizeHeliusUsesSourceBackedSymbolForDisplayAndPricing(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:           "sol-swap-symbols",
		Timestamp:    1700000000,
		Source:       types.SourceHelius,
		Chain:        types.ChainSolana,
		Wallet:       "wallet",
		Asset:        "mint-in",
		AssetSymbol:  "usdc",
		Amount:       "10.5",
		Asset2:       "mint-out",
		Asset2Symbol: "usdt",
		Amount2:      "9.75",
		RawType:      "SWAP/JUPITER",
	}, map[string]bool{"wallet": true}, price.NewProvider())
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxSwap {
		t.Fatalf("expected tx_type %q, got %q", types.TxSwap, tx.TxType)
	}
	if tx.Sent == nil || tx.Received == nil {
		t.Fatalf("expected swap legs, got sent=%#v received=%#v", tx.Sent, tx.Received)
	}
	if tx.Sent.Asset != "USDC" || tx.Sent.USDValue != "10.50000000" {
		t.Fatalf("unexpected sent leg: %#v", tx.Sent)
	}
	if tx.Received.Asset != "USDT" || tx.Received.USDValue != "9.75000000" {
		t.Fatalf("unexpected received leg: %#v", tx.Received)
	}
}

func TestNormalizeWithDiagnosticsSkipsRowWithMissingWallet(t *testing.T) {
	t.Parallel()

	result := NormalizeWithDiagnostics([]fetcher.RawTransaction{
		{
			ID:        "missing-wallet-tx",
			Timestamp: 1700000000,
			Source:    types.SourceEtherscan,
			Chain:     types.ChainEthereum,
			Wallet:    "",
			FromAddr:  "0xaaa",
			ToAddr:    "0xbbb",
			Asset:     "ETH",
			Amount:    "1.0",
			RawType:   "transfer",
		},
	}, []string{"0xaaa"}, nil)

	if len(result.Transactions) != 0 {
		t.Fatalf("expected 0 normalized transactions, got %d", len(result.Transactions))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped row, got %d", len(result.Skipped))
	}
	s := result.Skipped[0]
	if s.TxID != "missing-wallet-tx" {
		t.Fatalf("expected skipped tx_id %q, got %q", "missing-wallet-tx", s.TxID)
	}
	if s.Source != types.SourceEtherscan {
		t.Fatalf("expected skipped source %q, got %q", types.SourceEtherscan, s.Source)
	}
	if s.Chain != types.ChainEthereum {
		t.Fatalf("expected skipped chain %q, got %q", types.ChainEthereum, s.Chain)
	}
	if s.RawType != "transfer" {
		t.Fatalf("expected skipped raw_type %q, got %q", "transfer", s.RawType)
	}
	if s.Reason == "" {
		t.Fatal("expected non-empty skip reason")
	}
}

func TestNormalizeWithDiagnosticsPartitionsValidAndInvalidRows(t *testing.T) {
	t.Parallel()

	result := NormalizeWithDiagnostics([]fetcher.RawTransaction{
		{
			ID:        "good-tx",
			Timestamp: 1700000000,
			Source:    types.SourceRobinhood,
			Chain:     types.ChainRobinhood,
			Wallet:    "rh-account",
			Asset:     "BTC",
			Amount:    "0.5",
			USDPrice:  "15000",
			RawType:   "BUY",
		},
		{
			ID:        "bad-tx",
			Timestamp: 1700000000,
			Source:    types.SourceHelius,
			Chain:     types.ChainSolana,
			Wallet:    "",
			Asset:     "SOL",
			Amount:    "1.0",
			RawType:   "TRANSFER",
		},
		{
			ID:        "another-good-tx",
			Timestamp: 1700000001,
			Source:    types.SourceRobinhood,
			Chain:     types.ChainRobinhood,
			Wallet:    "rh-account",
			Asset:     "ETH",
			Amount:    "2.0",
			USDPrice:  "4000",
			RawType:   "BUY",
		},
	}, []string{"rh-account"}, nil)

	if len(result.Transactions) != 2 {
		t.Fatalf("expected 2 normalized transactions, got %d", len(result.Transactions))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped row, got %d", len(result.Skipped))
	}
	if result.Transactions[0].ID != "good-tx" {
		t.Fatalf("expected first tx ID %q, got %q", "good-tx", result.Transactions[0].ID)
	}
	if result.Transactions[1].ID != "another-good-tx" {
		t.Fatalf("expected second tx ID %q, got %q", "another-good-tx", result.Transactions[1].ID)
	}
	if result.Skipped[0].TxID != "bad-tx" {
		t.Fatalf("expected skipped tx ID %q, got %q", "bad-tx", result.Skipped[0].TxID)
	}
}

func TestNormalizeWithDiagnosticsEmptyInput(t *testing.T) {
	t.Parallel()

	result := NormalizeWithDiagnostics(nil, nil, nil)

	if len(result.Transactions) != 0 {
		t.Fatalf("expected 0 transactions, got %d", len(result.Transactions))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestNormalizeBackwardCompatibleWithSkippedRows(t *testing.T) {
	t.Parallel()

	txs, err := Normalize([]fetcher.RawTransaction{
		{
			ID:        "good-tx",
			Timestamp: 1700000000,
			Source:    types.SourceRobinhood,
			Chain:     types.ChainRobinhood,
			Wallet:    "rh-account",
			Asset:     "BTC",
			Amount:    "0.5",
			USDPrice:  "15000",
			RawType:   "BUY",
		},
		{
			ID:        "bad-tx",
			Timestamp: 1700000000,
			Source:    types.SourceHelius,
			Chain:     types.ChainSolana,
			Wallet:    "",
			Asset:     "SOL",
			Amount:    "1.0",
		},
	}, []string{"rh-account"}, nil)
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}

	if len(txs) != 1 {
		t.Fatalf("expected 1 normalized transaction, got %d", len(txs))
	}
	if txs[0].ID != "good-tx" {
		t.Fatalf("expected tx ID %q, got %q", "good-tx", txs[0].ID)
	}
}

func TestNormalizeHyperliquidPerpOpenEmitsPerpOpenType(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:            "fill-open-long",
		Timestamp:     time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC).Unix(),
		Source:        types.SourceHyperliquid,
		Chain:         types.ChainHyperliquid,
		Wallet:        "0xwallet",
		EventGroupID:  "hyperliquid:fill:fill-open-long",
		SplitReason:   "api_fill_granularity",
		Asset:         "BTC",
		Amount:        "0.0004",
		USDPrice:      "124825",
		RawType:       "Open Long",
		ClosedPnl:     "0.0",
		StartPosition: "0.0",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxPerpOpen {
		t.Fatalf("expected tx_type %q, got %q", types.TxPerpOpen, tx.TxType)
	}
	if tx.Received == nil {
		t.Fatal("expected received leg for perp open")
	}
	if tx.Received.Asset != "BTC" {
		t.Fatalf("expected asset BTC, got %q", tx.Received.Asset)
	}
	if tx.ClosedPnl != nil {
		t.Fatalf("expected no closed_pnl on open, got %q", *tx.ClosedPnl)
	}
	if tx.EventGroupID == nil || *tx.EventGroupID != "hyperliquid:fill:fill-open-long" {
		t.Fatalf("expected event_group_id %q, got %#v", "hyperliquid:fill:fill-open-long", tx.EventGroupID)
	}
	if tx.SplitReason == nil || *tx.SplitReason != "api_fill_granularity" {
		t.Fatalf("expected split_reason %q, got %#v", "api_fill_granularity", tx.SplitReason)
	}
}

func TestNormalizeHyperliquidPerpCloseEmitsPerpCloseWithClosedPnl(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:            "fill-close-short",
		Timestamp:     time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC).Unix(),
		Source:        types.SourceHyperliquid,
		Chain:         types.ChainHyperliquid,
		Wallet:        "0xwallet",
		EventGroupID:  "hyperliquid:fill:fill-close-short",
		SplitReason:   "api_fill_granularity",
		Asset:         "SOL",
		Amount:        "10.5",
		USDPrice:      "200",
		RawType:       "Close Short",
		ClosedPnl:     "-42.50",
		StartPosition: "10.5",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxPerpClose {
		t.Fatalf("expected tx_type %q, got %q", types.TxPerpClose, tx.TxType)
	}
	if tx.Sent == nil {
		t.Fatal("expected sent leg for perp close")
	}
	if tx.Sent.Asset != "SOL" {
		t.Fatalf("expected asset SOL, got %q", tx.Sent.Asset)
	}
	if tx.ClosedPnl == nil {
		t.Fatal("expected closed_pnl on perp close")
	}
	if *tx.ClosedPnl != "-42.50" {
		t.Fatalf("expected closed_pnl %q, got %q", "-42.50", *tx.ClosedPnl)
	}
	if tx.EventGroupID == nil || *tx.EventGroupID != "hyperliquid:fill:fill-close-short" {
		t.Fatalf("expected event_group_id %q, got %#v", "hyperliquid:fill:fill-close-short", tx.EventGroupID)
	}
	if tx.SplitReason == nil || *tx.SplitReason != "api_fill_granularity" {
		t.Fatalf("expected split_reason %q, got %#v", "api_fill_granularity", tx.SplitReason)
	}
}

func TestNormalizeHyperliquidFundingCarriesMarketContext(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:           "0x0000000000000000000000000000000000000000000000000000000000000000",
		Timestamp:    time.Date(2025, 10, 7, 0, 0, 0, 0, time.UTC).Unix(),
		Source:       types.SourceHyperliquid,
		Chain:        types.ChainHyperliquid,
		Wallet:       "0xwallet",
		Asset:        "BTC",
		Market:       "BTC",
		Amount:       "-0.168095",
		RawType:      "funding",
		EventGroupID: "hyperliquid:funding:0xwallet:1759795200000:BTC:-0.168095",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxFundingPayment {
		t.Fatalf("expected tx_type %q, got %q", types.TxFundingPayment, tx.TxType)
	}
	if tx.Market == nil || *tx.Market != "BTC" {
		t.Fatalf("expected market %q, got %#v", "BTC", tx.Market)
	}
	if tx.EventGroupID == nil || *tx.EventGroupID != "hyperliquid:funding:0xwallet:1759795200000:BTC:-0.168095" {
		t.Fatalf("unexpected event_group_id: %#v", tx.EventGroupID)
	}
	if tx.Sent == nil || tx.Sent.Asset != "USDC" {
		t.Fatalf("expected sent USDC funding leg, got %#v", tx.Sent)
	}
}

func TestNormalizeHeliusAssetCanonicalPopulatedWhenSymbolPresent(t *testing.T) {
	t.Parallel()

	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:          "sol-transfer-usdc",
		Timestamp:   1700000000,
		Source:      types.SourceHelius,
		Chain:       types.ChainSolana,
		Wallet:      "wallet",
		FromAddr:    "sender",
		ToAddr:      "wallet",
		Asset:       "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		AssetSymbol: "USDC",
		Amount:      "100.0",
		RawType:     "TRANSFER",
	}, map[string]bool{"wallet": true}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.Received == nil {
		t.Fatal("expected received leg")
	}
	if tx.Received.Asset != "USDC" {
		t.Fatalf("expected display asset USDC, got %q", tx.Received.Asset)
	}
	if tx.Received.AssetCanonical == nil {
		t.Fatal("expected asset_canonical to be populated when symbol differs from mint")
	}
	if *tx.Received.AssetCanonical != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" {
		t.Fatalf("expected asset_canonical to be the mint, got %q", *tx.Received.AssetCanonical)
	}
}

func TestNormalizeHeliusAssetCanonicalNilWhenNoSymbol(t *testing.T) {
	t.Parallel()

	mint := "7GCihgDB8fe6KNjn2MYtkzZcRjQy3t9GHdC8uHYmW2hr"
	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:        "sol-transfer-mint-only",
		Timestamp: 1700000000,
		Source:    types.SourceHelius,
		Chain:     types.ChainSolana,
		Wallet:    "wallet",
		FromAddr:  "sender",
		ToAddr:    "wallet",
		Asset:     mint,
		Amount:    "50.0",
		RawType:   "TRANSFER",
	}, map[string]bool{"wallet": true}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.Received == nil {
		t.Fatal("expected received leg")
	}
	if tx.Received.Asset != mint {
		t.Fatalf("expected display asset to be mint, got %q", tx.Received.Asset)
	}
	if tx.Received.AssetCanonical != nil {
		t.Fatalf("expected asset_canonical nil when display=mint, got %q", *tx.Received.AssetCanonical)
	}
}

func TestNormalizeHeliusPumpfunMintRemainsConservativeWithoutSourceSymbol(t *testing.T) {
	t.Parallel()

	mint := "CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP"
	tx, err := normalizeOne(fetcher.RawTransaction{
		ID:        "PbxPFcX7JQF6PuTMjRs2xKuC2azpAmnc1uALwYxCKuwnWFT3vb156p17CZRYjcU1ySB1ANHSWgGfPEfHtE2QXKm",
		Timestamp: time.Date(2025, 8, 3, 19, 22, 35, 0, time.UTC).Unix(),
		Source:    types.SourceHelius,
		Chain:     types.ChainSolana,
		Wallet:    "BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp",
		Asset:     "SOL",
		Amount:    "0.000803279",
		Asset2:    mint,
		Amount2:   "540724.686218000",
		Fee:       "0.001005000",
		FeeAsset:  "SOL",
		RawType:   "SWAP/PUMP_FUN",
	}, map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("normalizeOne returned error: %v", err)
	}

	if tx.TxType != types.TxSwap {
		t.Fatalf("expected tx_type %q, got %q", types.TxSwap, tx.TxType)
	}
	if tx.Received == nil {
		t.Fatal("expected received leg")
	}
	if tx.Received.Asset != mint {
		t.Fatalf("expected unresolved received mint %q, got %q", mint, tx.Received.Asset)
	}
	if tx.Received.USDValue != "0" {
		t.Fatalf("expected unresolved received usd_value 0, got %q", tx.Received.USDValue)
	}
}
