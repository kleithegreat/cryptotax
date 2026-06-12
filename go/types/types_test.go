package types

import (
	"testing"
	"time"
)

func TestCanonicalWallet(t *testing.T) {
	mixed := "0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA"
	if got := CanonicalWallet(mixed); got != "0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca" {
		t.Errorf("EVM address not folded: %q", got)
	}
	sol := "BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp"
	if got := CanonicalWallet(sol); got != sol {
		t.Errorf("Solana address must stay exact: %q", got)
	}
	if got := CanonicalWallet("  robinhood "); got != "robinhood" {
		t.Errorf("trim failed: %q", got)
	}
}

// Same-second rows must order acquisitions before disposals so FIFO sees
// the lot before the sale (Robinhood synthesizes same-day midnight rows).
func TestSortTransactionsAcquisitionsBeforeDisposals(t *testing.T) {
	ts := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	txs := []Transaction{
		{ID: "sell", Timestamp: ts, TxType: TxSell},
		{ID: "buy", Timestamp: ts, TxType: TxBuy},
		{ID: "earlier", Timestamp: ts.Add(-time.Hour), TxType: TxSell},
	}
	SortTransactions(txs)
	if txs[0].ID != "earlier" || txs[1].ID != "buy" || txs[2].ID != "sell" {
		t.Errorf("order = %s,%s,%s; want earlier,buy,sell", txs[0].ID, txs[1].ID, txs[2].ID)
	}
}

func TestSortTransactionsDeterministicTiebreak(t *testing.T) {
	ts := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	txs := []Transaction{
		{ID: "b", Timestamp: ts, TxType: TxBuy, Wallet: "w2"},
		{ID: "a", Timestamp: ts, TxType: TxBuy, Wallet: "w1"},
	}
	SortTransactions(txs)
	if txs[0].ID != "a" {
		t.Errorf("expected ID tiebreak, got %s first", txs[0].ID)
	}
}
