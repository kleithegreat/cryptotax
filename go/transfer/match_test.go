package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestMatchTransfersRelabelsConfirmedBridgePair(t *testing.T) {
	t.Parallel()

	bridgeOut := mustLoadPayload(t, filepath.Join("..", "audit", "testdata", "real-wallet", "eth-bridge-out-usdc.input.json"))
	bridgeIn := mustLoadPayload(t, filepath.Join("..", "audit", "testdata", "real-wallet", "arb-bridge-in-fillrelay.input.json"))

	txs := append([]types.Transaction{}, bridgeOut.Transactions...)
	txs = append(txs, bridgeIn.Transactions...)

	matched := MatchTransfers(txs, bridgeOut.Wallets)

	if matched[0].TxType != types.TxTransferOut {
		t.Fatalf("expected confirmed bridge out to become transfer_out, got %q", matched[0].TxType)
	}
	if matched[1].TxType != types.TxSell {
		t.Fatalf("expected zero-amount ETH bridge artifact to remain sell, got %q", matched[1].TxType)
	}
	if matched[2].TxType != types.TxTransferIn {
		t.Fatalf("expected confirmed bridge completion to stay transfer_in, got %q", matched[2].TxType)
	}
}

func TestMatchTransfersDoesNotRelabelSwapLikeCrossChainLookalike(t *testing.T) {
	t.Parallel()

	bridgeOut := mustLoadPayload(t, filepath.Join("..", "audit", "testdata", "real-wallet", "eth-bridge-out-usdc.input.json"))
	bridgeIn := mustLoadPayload(t, filepath.Join("..", "audit", "testdata", "real-wallet", "arb-bridge-in-fillrelay.input.json"))

	swapLikeOut := bridgeOut.Transactions[0]
	rawType := "swap(string aggregatorId, address tokenFrom, uint256 amount, bytes data)"
	swapLikeOut.RawType = &rawType

	matched := MatchTransfers([]types.Transaction{swapLikeOut, bridgeIn.Transactions[0]}, bridgeOut.Wallets)

	if matched[0].TxType != types.TxSell {
		t.Fatalf("expected swap-like outbound row to remain sell, got %q", matched[0].TxType)
	}
	if matched[1].TxType != types.TxTransferIn {
		t.Fatalf("expected inbound row to remain transfer_in when outbound leg is swap-like, got %q", matched[1].TxType)
	}
}

func mustLoadPayload(t *testing.T, path string) types.TxPayload {
	t.Helper()

	payloadJSON, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read payload %s: %v", path, err)
	}

	var payload types.TxPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("unmarshal payload %s: %v", path, err)
	}

	return payload
}
