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
