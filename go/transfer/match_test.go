package transfer

import (
	"testing"
	"time"

	"github.com/kevin/cryptotax/types"
)

func TestMatchTransfersDoesNotRelabelSellByAmountAlone(t *testing.T) {
	t.Parallel()

	externalOut := "0xexternal-out"
	externalIn := "0xexternal-in"
	now := time.Unix(1700000000, 0).UTC()

	txs := []types.Transaction{
		{
			ID:           "sell",
			Timestamp:    now,
			TxType:       types.TxSell,
			Wallet:       "0xaaa",
			Counterparty: &externalOut,
			Sent: &types.AssetAmount{
				Asset:    "ETH",
				Amount:   "1.0",
				USDValue: "1000",
			},
		},
		{
			ID:           "inbound",
			Timestamp:    now.Add(5 * time.Minute),
			TxType:       types.TxTransferIn,
			Wallet:       "0xbbb",
			Counterparty: &externalIn,
			Received: &types.AssetAmount{
				Asset:    "ETH",
				Amount:   "1.0",
				USDValue: "1000",
			},
		},
	}

	matched := MatchTransfers(txs, []string{"0xaaa", "0xbbb"})
	if matched[0].TxType != types.TxSell {
		t.Fatalf("expected sell to remain sell, got %q", matched[0].TxType)
	}
	if matched[1].TxType != types.TxTransferIn {
		t.Fatalf("expected inbound transfer to remain transfer_in, got %q", matched[1].TxType)
	}
}
