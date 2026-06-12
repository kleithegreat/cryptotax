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

// Substring matching used to misidentify these; resolution must be exact.
func TestResolveCryptoSymbolExactness(t *testing.T) {
	cases := map[string]string{
		"Bitcoin":              "BTC",
		"Bitcoin BTC":          "BTC",
		"BITCOIN":              "BTC",
		"Solana SOL":           "SOL",
		"USDC":                 "USDC",
		"USD Coin USDC":        "USDC",
		"Shiba Inu SHIB":       "SHIB",
		"Bitcoin Cash":         "", // must NOT collapse into BTC
		"Bitcoin Cash BCH":     "",
		"Ethereum Classic":     "",
		"Ethereum Classic ETC": "",
		"Wrapped Bitcoin":      "",
		"Litecoin LTC":         "",
		"":                     "",
	}
	for name, want := range cases {
		if got := resolveCryptoSymbol(name); got != want {
			t.Errorf("resolveCryptoSymbol(%q) = %q, want %q", name, got, want)
		}
	}
}

// Unmapped assets must be preserved verbatim, not dropped.
func TestParseDARowPreservesUnmappedAssets(t *testing.T) {
	r := NewRobinhood("unused")
	header := []string{"1099-DA", "ACCOUNT NUMBER", "TAX YEAR", "DATE ACQUIRED", "SALE DATE", "DTIF NAME", "DTIF UNITS", "COST BASIS", "SALES PRICE", "TERM"}
	colIdx := map[string]int{}
	for i, col := range header[1:] {
		colIdx[col] = i + 1
	}

	row := []string{"1099-DA", "ACC1", "2025", "01/15/2025", "03/20/2025", "Litecoin LTC", "2.5", "$100.00", "$150.00", "SHORT"}
	txs := r.parseDARow(7, row, colIdx)
	if len(txs) != 2 {
		t.Fatalf("got %d rows, want buy+sell", len(txs))
	}
	for _, tx := range txs {
		if tx.Asset != "Litecoin LTC" {
			t.Errorf("asset = %q, want verbatim DTIF name", tx.Asset)
		}
	}
}

// "VARIOUS" acquisition dates must not abort the fetch; the disposition leg
// is still emitted so the missing basis surfaces loudly in the core.
func TestParseDARowVariousAcquiredDate(t *testing.T) {
	r := NewRobinhood("unused")
	header := []string{"1099-DA", "ACCOUNT NUMBER", "DATE ACQUIRED", "SALE DATE", "DTIF NAME", "DTIF UNITS", "COST BASIS", "SALES PRICE"}
	colIdx := map[string]int{}
	for i, col := range header[1:] {
		colIdx[col] = i + 1
	}

	row := []string{"1099-DA", "ACC1", "VARIOUS", "03/20/2025", "Solana SOL", "10", "$500.00", "$900.00"}
	txs := r.parseDARow(3, row, colIdx)
	if len(txs) != 1 {
		t.Fatalf("got %d rows, want only the disposition", len(txs))
	}
	if txs[0].RawType != "1099-DA-SELL" || txs[0].Asset != "SOL" {
		t.Errorf("unexpected row: %+v", txs[0])
	}
}

// A consolidated 1099 without a 1099-DA section must error, not silently
// produce zero transactions.
func TestParse1099DAMissingSectionErrors(t *testing.T) {
	r := NewRobinhood("test.csv")
	records := [][]string{
		{"1099-B", "ACCOUNT NUMBER", "DESCRIPTION"},
		{"1099-B", "ACC1", "SOME ETF"},
	}
	if _, err := r.parse1099DA(records); err == nil {
		t.Fatal("expected error for missing 1099-DA section")
	}
}
