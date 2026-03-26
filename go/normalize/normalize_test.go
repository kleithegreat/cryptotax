package normalize

import (
	"testing"
	"time"

	"github.com/kevin/cryptotax/fetcher"
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
