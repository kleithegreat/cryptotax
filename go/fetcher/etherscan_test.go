package fetcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kevin/cryptotax/types"
)

func TestParseEtherscanResultHandlesStringErrorsAndEmptyResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		envelope  etherscanResp
		wantErr   string
		wantEmpty bool
	}{
		{
			name: "no transactions found",
			envelope: etherscanResp{
				Status:  "0",
				Message: "No transactions found",
				Result:  json.RawMessage(`"No transactions found"`),
			},
			wantEmpty: true,
		},
		{
			name: "api error string result",
			envelope: etherscanResp{
				Status:  "0",
				Message: "NOTOK",
				Result:  json.RawMessage(`"Invalid API Key"`),
			},
			wantErr: `status="0" message="NOTOK" result="Invalid API Key"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			txs, exhausted, err := parseEtherscanResult(tt.envelope)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !exhausted {
					t.Fatalf("expected exhausted=true")
				}
				if !tt.wantEmpty || len(txs) != 0 {
					t.Fatalf("expected no transactions, got %d", len(txs))
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestEtherscanFetchEndpointPaginates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page query: %v", err)
			return
		}

		type response struct {
			Status  string        `json:"status"`
			Message string        `json:"message"`
			Result  []etherscanTx `json:"result"`
		}

		var txs []etherscanTx
		switch page {
		case 1:
			txs = make([]etherscanTx, etherscanPageSize)
			for i := range txs {
				txs[i] = etherscanTx{
					Hash:      "0xpage1-" + strconv.Itoa(i),
					TimeStamp: strconv.Itoa(1700000000 + i),
					From:      "0xaaa",
					To:        "0xbbb",
					Value:     "1000000000000000000",
					GasPrice:  "1",
					GasUsed:   "21000",
				}
			}
		case 2:
			txs = []etherscanTx{{
				Hash:      "0xpage2-0",
				TimeStamp: "1700001000",
				From:      "0xaaa",
				To:        "0xbbb",
				Value:     "1000000000000000000",
				GasPrice:  "1",
				GasUsed:   "21000",
			}}
		default:
			t.Errorf("unexpected page %d", page)
			return
		}

		if err := json.NewEncoder(w).Encode(response{
			Status:  "1",
			Message: "OK",
			Result:  txs,
		}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := NewEtherscan("test-key", 1, types.ChainEthereum)
	client.BaseURL = server.URL

	txs, err := client.fetchEndpoint("0xaaa", "txlist")
	if err != nil {
		t.Fatalf("fetchEndpoint returned error: %v", err)
	}

	if len(txs) != etherscanPageSize+1 {
		t.Fatalf("expected %d transactions, got %d", etherscanPageSize+1, len(txs))
	}
	if txs[0].Wallet != "0xaaa" {
		t.Fatalf("expected wallet to be propagated, got %q", txs[0].Wallet)
	}
	if txs[len(txs)-1].ID != "0xpage2-0" {
		t.Fatalf("expected final paginated tx to be present, got %q", txs[len(txs)-1].ID)
	}
}

func TestEtherscanFetchPageRetriesRateLimitResponses(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			if err := json.NewEncoder(w).Encode(map[string]any{
				"status":  "0",
				"message": "NOTOK",
				"result":  "Max calls per sec rate limit reached (3/sec)",
			}); err != nil {
				t.Errorf("encoding rate-limit response: %v", err)
			}
			return
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "1",
			"message": "OK",
			"result": []etherscanTx{{
				Hash:      "0xretried",
				TimeStamp: "1700000000",
				From:      "0xaaa",
				To:        "0xbbb",
				Value:     "1000000000000000000",
				GasPrice:  "1",
				GasUsed:   "21000",
			}},
		}); err != nil {
			t.Errorf("encoding success response: %v", err)
		}
	}))
	defer server.Close()

	client := NewEtherscan("test-key", 1, types.ChainEthereum)
	client.BaseURL = server.URL
	client.Sleep = func(time.Duration) {}

	txs, exhausted, err := client.fetchPage("0xaaa", "txlist", 1)
	if err != nil {
		t.Fatalf("fetchPage returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests after retry, got %d", requests)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}
	if txs[0].ID != "0xretried" {
		t.Fatalf("expected retried transaction id, got %q", txs[0].ID)
	}
	if exhausted != true {
		t.Fatalf("expected exhausted=true after short page")
	}
}

// assembleRows fee attribution: gas belongs to the initiator and lands on
// exactly one surviving row per hash.
func TestAssembleRowsFeeAttribution(t *testing.T) {
	t.Parallel()

	wallet := "0xAAA"
	normal := []RawTransaction{
		// Wallet-initiated contract call: zero-value artifact row.
		{ID: "0xswap", FromAddr: "0xaaa", ToAddr: "0xrouter", Asset: "ETH", Amount: "0", Fee: "0.01"},
		// Incoming ETH from someone else: their gas, not ours.
		{ID: "0xin", FromAddr: "0xother", ToAddr: "0xaaa", Asset: "ETH", Amount: "1", Fee: "0.02"},
	}
	tokens := []RawTransaction{
		// Two token legs of the wallet-initiated swap.
		{ID: "0xswap", FromAddr: "0xaaa", ToAddr: "0xpool", Asset: "USDC", Amount: "100", Fee: "0.01"},
		{ID: "0xswap", FromAddr: "0xpool", ToAddr: "0xaaa", Asset: "WETH", Amount: "0.05", Fee: "0.01"},
		// Token pulled by an approved spender in a tx we did not initiate.
		{ID: "0xpull", FromAddr: "0xaaa", ToAddr: "0xspender", Asset: "DAI", Amount: "50", Fee: "0.03"},
	}

	rows := assembleRows(wallet, normal, tokens)

	var feeCount int
	for _, row := range rows {
		if row.ID == "0xswap" && row.Amount == "0" {
			t.Error("zero-value artifact row should be dropped when token rows exist")
		}
		if row.Fee != "" {
			feeCount++
			if row.ID != "0xswap" || row.Asset != "USDC" {
				t.Errorf("fee landed on %s/%s, want the nonzero outbound USDC leg of 0xswap", row.ID, row.Asset)
			}
		}
	}
	if feeCount != 1 {
		t.Errorf("fee appears on %d rows, want exactly 1", feeCount)
	}
}

// Token rows must carry the contract address as canonical identity.
func TestEtherscanTokenRowsCarryContractAddress(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result []etherscanTx
		if r.URL.Query().Get("action") == "tokentx" {
			result = []etherscanTx{{
				Hash:            "0xtok",
				TimeStamp:       "1700000000",
				From:            "0xaaa",
				To:              "0xbbb",
				Value:           "5000000",
				TokenSymbol:     "USDC",
				TokenDecimal:    "6",
				ContractAddress: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
				GasPrice:        "1",
				GasUsed:         "21000",
			}}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "1", "message": "OK", "result": result}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := NewEtherscan("test-key", 1, types.ChainEthereum)
	client.BaseURL = server.URL

	txs, err := client.Fetch("0xaaa")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 row, got %d", len(txs))
	}
	if txs[0].AssetCanonical != "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" {
		t.Errorf("canonical = %q, want lowercased contract address", txs[0].AssetCanonical)
	}
	if txs[0].Amount != "5" {
		t.Errorf("amount = %q, want 5", txs[0].Amount)
	}
}
