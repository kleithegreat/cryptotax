package fetcher

import (
	"testing"
	"time"
)

func TestRobinhoodParses1099DATimestamps(t *testing.T) {
	t.Parallel()

	records := [][]string{
		{"1099-DA", "ACCOUNT NUMBER", "TAX YEAR", "DATE ACQUIRED", "SALE DATE", "DESCRIPTION", "DTIF CODE", "DTIF NAME", "DTIF UNITS", "COST BASIS", "SALES PRICE", "TERM"},
		{"1099-DA", "acct", "2024", "01/15/2024", "02/20/2024", "", "", "Bitcoin BTC", "0.50000000", "$12,345.67", "$13,456.78", "SHORT"},
	}

	txs, err := NewRobinhood("ignored").parse1099DA(records)
	if err != nil {
		t.Fatalf("parse1099DA returned error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 synthetic transactions, got %d", len(txs))
	}

	wantBuyTS := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC).Unix()
	wantSellTS := time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC).Unix()

	if txs[0].Timestamp != wantBuyTS {
		t.Fatalf("expected buy timestamp %d, got %d", wantBuyTS, txs[0].Timestamp)
	}
	if txs[1].Timestamp != wantSellTS {
		t.Fatalf("expected sell timestamp %d, got %d", wantSellTS, txs[1].Timestamp)
	}
	if txs[0].Wallet != "robinhood" || txs[1].Wallet != "robinhood" {
		t.Fatalf("expected robinhood wallet on synthetic rows, got %q and %q", txs[0].Wallet, txs[1].Wallet)
	}
	if txs[0].USDPrice != "12345.67" || txs[1].USDPrice != "13456.78" {
		t.Fatalf("expected cleaned USD values, got %q and %q", txs[0].USDPrice, txs[1].USDPrice)
	}
}
