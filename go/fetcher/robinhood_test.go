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
	if txs[0].EventGroupID == "" || txs[0].EventGroupID != txs[1].EventGroupID {
		t.Fatalf("expected shared event group id on synthetic rows, got %q and %q", txs[0].EventGroupID, txs[1].EventGroupID)
	}
	if txs[0].SplitReason != "synthetic_1099da_row" || txs[1].SplitReason != "synthetic_1099da_row" {
		t.Fatalf("expected synthetic split reason, got %q and %q", txs[0].SplitReason, txs[1].SplitReason)
	}
}

func TestParseRobinhoodDateSupportsCompactDates(t *testing.T) {
	t.Parallel()

	got, err := parseRobinhoodDate("20250915")
	if err != nil {
		t.Fatalf("parseRobinhoodDate returned error: %v", err)
	}

	want := time.Date(2025, time.September, 15, 0, 0, 0, 0, time.UTC).Unix()
	if got != want {
		t.Fatalf("expected %d, got %d", want, got)
	}
}
