package fetcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kevin/cryptotax/types"
)

func TestHeliusGetSignaturesReturnsRPCError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-key"); got != "test-key" {
			t.Errorf("expected api key query, got %q", got)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32005,
				"message": "rate limited",
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	h := NewHelius("test-key")
	h.RPCURL = server.URL

	_, err := h.getSignatures("wallet")
	if err == nil {
		t.Fatal("expected RPC error, got nil")
	}
	if !strings.Contains(err.Error(), "RPC error -32005: rate limited") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeliusParseEnhancedUsesTransactionsEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-key"); got != "test-key" {
			t.Errorf("expected api key query, got %q", got)
			return
		}

		var body struct {
			Transactions []string `json:"transactions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		if len(body.Transactions) != 2 || body.Transactions[0] != "sig-1" || body.Transactions[1] != "sig-2" {
			t.Errorf("unexpected transactions envelope: %#v", body.Transactions)
			return
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
				TokenAmount:     json.Number("1.25"),
			}},
		}}); err != nil {
			t.Errorf("encode response: %v", err)
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
				TokenAmount:     json.Number("1"),
			}},
		}}); err != nil {
			t.Errorf("encode legacy response: %v", err)
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
			TokenAmount:     json.Number("540724.686218"),
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
			TokenAmount:     json.Number("1.25"),
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

// Regression for the last-wins leg-selection bug: a token→token swap with a
// rent-sized native SOL out-leg must keep the TOKEN legs, not let the SOL
// dust overwrite the sent side.
func TestConvertHeliusSwapNetsLegsAndIgnoresRentDust(t *testing.T) {
	t.Parallel()

	usdc := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	bonk := "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-route",
		Type:      "SWAP",
		Source:    "JUPITER",
		Fee:       5000,
		FeePayer:  "wallet",
		Timestamp: 1700000000,
		TokenTransfers: []heliusTokenTransfer{
			// Multi-hop: two partial USDC out-legs to different pools.
			{FromUserAccount: "wallet", ToUserAccount: "pool1", Mint: usdc, TokenAmount: json.Number("60")},
			{FromUserAccount: "wallet", ToUserAccount: "pool2", Mint: usdc, TokenAmount: json.Number("40")},
			{FromUserAccount: "pool2", ToUserAccount: "wallet", Mint: bonk, TokenAmount: json.Number("5000000")},
		},
		NativeTransfers: []heliusNativeTransfer{
			// Rent for a fresh token account: must not become the sent leg.
			{FromUserAccount: "wallet", ToUserAccount: "newata", Amount: 2039280},
		},
	}, "wallet")

	if len(txs) != 1 {
		t.Fatalf("expected 1 swap row, got %d", len(txs))
	}
	tx := txs[0]
	if tx.Asset != usdc || tx.Amount != "100" {
		t.Fatalf("sent leg = %s %s, want 100 USDC mint", tx.Amount, tx.Asset)
	}
	if tx.Asset2 != bonk || tx.Amount2 != "5000000" {
		t.Fatalf("received leg = %s %s, want 5000000 BONK mint", tx.Amount2, tx.Asset2)
	}
	if tx.Fee != "0.000005" || tx.FeeAsset != "SOL" {
		t.Fatalf("fee = %q %q, want 0.000005 SOL (wallet is fee payer)", tx.Fee, tx.FeeAsset)
	}
}

// A swap that is not a clean two-asset exchange must preserve every leg
// rather than guessing or dropping the event.
func TestConvertHeliusSwapFallsBackToLegPreservation(t *testing.T) {
	t.Parallel()

	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-multi",
		Type:      "SWAP",
		Source:    "JUPITER",
		Timestamp: 1700000000,
		TokenTransfers: []heliusTokenTransfer{
			{FromUserAccount: "wallet", ToUserAccount: "pool", Mint: "MintAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", TokenAmount: json.Number("10")},
			{FromUserAccount: "pool", ToUserAccount: "wallet", Mint: "MintBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", TokenAmount: json.Number("20")},
			{FromUserAccount: "pool", ToUserAccount: "wallet", Mint: "MintCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", TokenAmount: json.Number("30")},
		},
	}, "wallet")

	if len(txs) != 3 {
		t.Fatalf("expected 3 preserved legs, got %d", len(txs))
	}
	for _, tx := range txs {
		if tx.RawType != "SWAP" {
			t.Errorf("leg raw type = %q, want SWAP", tx.RawType)
		}
	}
}

// Fees on sponsored transactions belong to the sponsor, not the wallet.
func TestHeliusFeeRequiresWalletAsFeePayer(t *testing.T) {
	t.Parallel()

	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-sponsored",
		Type:      "TRANSFER",
		Fee:       5000,
		FeePayer:  "sponsor",
		Timestamp: 1700000000,
		TokenTransfers: []heliusTokenTransfer{{
			FromUserAccount: "wallet",
			ToUserAccount:   "dest",
			Mint:            "MintAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			TokenAmount:     json.Number("5"),
		}},
	}, "wallet")

	if len(txs) != 1 {
		t.Fatalf("expected 1 row, got %d", len(txs))
	}
	if txs[0].Fee != "" {
		t.Fatalf("fee = %q, want empty (sponsor paid)", txs[0].Fee)
	}
}

// Wrap/unwrap legs (wSOL mint vs native SOL) must net as one asset.
func TestConvertHeliusSwapMergesWrappedSOL(t *testing.T) {
	t.Parallel()

	usdc := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	txs := convertHeliusTx(heliusEnhancedTx{
		Signature: "sig-wsol",
		Type:      "SWAP",
		Source:    "RAYDIUM",
		FeePayer:  "wallet",
		Fee:       5000,
		Timestamp: 1700000000,
		TokenTransfers: []heliusTokenTransfer{
			// wSOL leg out of the wallet's temp account.
			{FromUserAccount: "wallet", ToUserAccount: "pool", Mint: wrappedSOLMint, TokenAmount: json.Number("1.5")},
			{FromUserAccount: "pool", ToUserAccount: "wallet", Mint: usdc, TokenAmount: json.Number("210")},
		},
		NativeTransfers: []heliusNativeTransfer{
			// Unwrap refund back to the wallet.
			{FromUserAccount: "tempacct", ToUserAccount: "wallet", Amount: 500000000},
		},
	}, "wallet")

	if len(txs) != 1 {
		t.Fatalf("expected 1 swap row, got %d", len(txs))
	}
	tx := txs[0]
	if tx.Asset != "SOL" || tx.Amount != "1" {
		t.Fatalf("sent leg = %s %s, want net 1 SOL", tx.Amount, tx.Asset)
	}
	if tx.Asset2 != usdc || tx.Amount2 != "210" {
		t.Fatalf("received leg = %s %s, want 210 USDC mint", tx.Amount2, tx.Asset2)
	}
}
