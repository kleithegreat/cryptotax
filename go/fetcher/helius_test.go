package fetcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kevin/cryptotax/types"
)

func TestHeliusParseEnhancedUsesTransactionsEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-key"); got != "test-key" {
			t.Fatalf("expected api key query, got %q", got)
		}

		var body struct {
			Transactions []string `json:"transactions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Transactions) != 2 || body.Transactions[0] != "sig-1" || body.Transactions[1] != "sig-2" {
			t.Fatalf("unexpected transactions envelope: %#v", body.Transactions)
		}

		if err := json.NewEncoder(w).Encode([]heliusEnhancedTx{{
			Signature: "sig-1",
			Type:      "TRANSFER",
			Fee:       5000,
			Timestamp: 1700000000,
			TokenTransfers: []heliusTokenTransfer{{
				FromUserAccount: "wallet",
				ToUserAccount:   "dest",
				Mint:            "So11111111111111111111111111111111111111112",
				TokenAmount:     1.25,
			}},
		}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	h := NewHelius("test-key")
	h.EnhancedURL = server.URL

	txs, err := h.parseEnhanced([]string{"sig-1", "sig-2"}, "wallet")
	if err != nil {
		t.Fatalf("parseEnhanced returned error: %v", err)
	}

	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}
	if txs[0].ID != "sig-1" {
		t.Fatalf("expected signature to propagate, got %q", txs[0].ID)
	}
	if txs[0].Source != types.SourceHelius {
		t.Fatalf("expected helius source, got %q", txs[0].Source)
	}
}

func TestHeliusParseEnhancedFallsBackToLegacyEndpoint(t *testing.T) {
	t.Parallel()

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error code: 1016", http.StatusNetworkAuthenticationRequired)
	}))
	defer primary.Close()

	legacyCalls := 0
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls++
		if err := json.NewEncoder(w).Encode([]heliusEnhancedTx{{
			Signature: "sig-legacy",
			Type:      "TRANSFER",
			Fee:       5000,
			Timestamp: 1700000000,
			TokenTransfers: []heliusTokenTransfer{{
				FromUserAccount: "wallet",
				ToUserAccount:   "dest",
				Mint:            "So11111111111111111111111111111111111111112",
				TokenAmount:     1,
			}},
		}}); err != nil {
			t.Fatalf("encode legacy response: %v", err)
		}
	}))
	defer legacy.Close()

	h := NewHelius("test-key")
	h.EnhancedURL = primary.URL
	h.LegacyEnhancedURL = legacy.URL

	txs, err := h.parseEnhanced([]string{"sig-legacy"}, "wallet")
	if err != nil {
		t.Fatalf("parseEnhanced returned error: %v", err)
	}
	if legacyCalls != 1 {
		t.Fatalf("expected 1 legacy fallback call, got %d", legacyCalls)
	}
	if len(txs) != 1 || txs[0].ID != "sig-legacy" {
		t.Fatalf("expected fallback transaction, got %#v", txs)
	}
}

func TestConvertHeliusSwapPreservesMintAndSourceSymbolSeparately(t *testing.T) {
	t.Parallel()

	mint := "CMMNJETQSDR79XaLkttgQjaQwuWqzuLifLJT8F7mpump"
	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-symbol",
		Type:      "SWAP",
		Source:    "PUMP_FUN",
		Fee:       5000,
		Timestamp: 1700000000,
		NativeTransfers: []heliusNativeTransfer{{
			FromUserAccount: "wallet",
			ToUserAccount:   "pool",
			Amount:          803279,
		}},
		TokenTransfers: []heliusTokenTransfer{{
			FromUserAccount: "pool",
			ToUserAccount:   "wallet",
			Mint:            mint,
			Symbol:          " pump ",
			TokenAmount:     540724.686218,
		}},
	}, "wallet")

	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}

	tx := txs[0]
	if tx.Asset != "SOL" || tx.AssetSymbol != "" {
		t.Fatalf("expected native sent leg to stay SOL without separate symbol, got asset=%q symbol=%q", tx.Asset, tx.AssetSymbol)
	}
	if tx.Asset2 != mint {
		t.Fatalf("expected received mint %q, got %q", mint, tx.Asset2)
	}
	if tx.Asset2Symbol != "pump" {
		t.Fatalf("expected trimmed source symbol %q, got %q", "pump", tx.Asset2Symbol)
	}
	if tx.RawType != "SWAP/PUMP_FUN" {
		t.Fatalf("expected raw type to preserve source, got %q", tx.RawType)
	}
}

func TestConvertHeliusTransferDoesNotInventDisplaySymbol(t *testing.T) {
	t.Parallel()

	mint := "So11111111111111111111111111111111111111112"
	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-transfer",
		Type:      "TRANSFER",
		Fee:       5000,
		Timestamp: 1700000000,
		TokenTransfers: []heliusTokenTransfer{{
			FromUserAccount: "sender",
			ToUserAccount:   "wallet",
			Mint:            mint,
			TokenAmount:     1.25,
		}},
	}, "wallet")

	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}

	tx := txs[0]
	if tx.Asset != mint {
		t.Fatalf("expected asset mint %q, got %q", mint, tx.Asset)
	}
	if tx.AssetSymbol != "" {
		t.Fatalf("expected blank display symbol when source omits it, got %q", tx.AssetSymbol)
	}
	if tx.EventGroupID != "sig-transfer" {
		t.Fatalf("expected event group id %q, got %q", "sig-transfer", tx.EventGroupID)
	}
	if tx.SplitReason != "wallet_touching_leg_preservation" {
		t.Fatalf("expected split reason %q, got %q", "wallet_touching_leg_preservation", tx.SplitReason)
	}
}
