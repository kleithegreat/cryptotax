package fetcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/kevin/cryptotax/types"
)

// Helius fetches Solana transaction history via the Helius API.
// Uses getSignaturesForAddress (RPC) + Enhanced Transactions API (batch parse).
type Helius struct {
	APIKey string
	Client *http.Client
}

const (
	heliusSignatureLimit = 1000
	heliusBatchSize      = 100
	heliusRateLimitDelay = 150 * time.Millisecond // ~7 req/s, well under 10 req/s
)

func NewHelius(apiKey string) *Helius {
	return &Helius{
		APIKey: apiKey,
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *Helius) Name() string { return "helius/solana" }

func (h *Helius) Fetch(wallet string) ([]RawTransaction, error) {
	// Step 1: Get all transaction signatures
	sigs, err := h.getSignatures(wallet)
	if err != nil {
		return nil, fmt.Errorf("getting signatures: %w", err)
	}

	if len(sigs) == 0 {
		return nil, nil
	}

	// Step 2: Parse via Enhanced Transactions API in batches of 100
	var allTxs []RawTransaction
	for i := 0; i < len(sigs); i += heliusBatchSize {
		end := i + heliusBatchSize
		if end > len(sigs) {
			end = len(sigs)
		}
		batch := sigs[i:end]

		txs, err := h.parseEnhanced(batch, wallet)
		if err != nil {
			return nil, fmt.Errorf("parsing batch %d: %w", i/heliusBatchSize, err)
		}
		allTxs = append(allTxs, txs...)

		time.Sleep(heliusRateLimitDelay)
	}

	return allTxs, nil
}

func (h *Helius) getSignatures(wallet string) ([]string, error) {
	var allSigs []string
	var before string
	rpcURL := fmt.Sprintf("https://mainnet.helius-rpc.com/?api-key=%s", h.APIKey)

	for {
		params := map[string]interface{}{
			"limit": heliusSignatureLimit,
		}
		if before != "" {
			params["before"] = before
		}

		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "getSignaturesForAddress",
			"params":  []interface{}{wallet, params},
		}

		respBody, err := h.post(rpcURL, body)
		if err != nil {
			return nil, fmt.Errorf("getSignaturesForAddress: %w", err)
		}

		var result struct {
			Result []struct {
				Signature string `json:"signature"`
				Err       any    `json:"err"`
			} `json:"result"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parsing signatures response: %w", err)
		}

		for _, sig := range result.Result {
			if sig.Err == nil {
				allSigs = append(allSigs, sig.Signature)
			}
		}

		if len(result.Result) < heliusSignatureLimit {
			break
		}
		before = result.Result[len(result.Result)-1].Signature

		time.Sleep(heliusRateLimitDelay)
	}

	return allSigs, nil
}

// Enhanced Transactions API response types
type heliusEnhancedTx struct {
	Signature       string                  `json:"signature"`
	Type            string                  `json:"type"`
	Source          string                  `json:"source"`
	Fee             int64                   `json:"fee"`
	Timestamp       int64                   `json:"timestamp"`
	TokenTransfers  []heliusTokenTransfer   `json:"tokenTransfers"`
	NativeTransfers []heliusNativeTransfer  `json:"nativeTransfers"`
	Description     string                  `json:"description"`
}

type heliusTokenTransfer struct {
	FromUserAccount string  `json:"fromUserAccount"`
	ToUserAccount   string  `json:"toUserAccount"`
	Mint            string  `json:"mint"`
	TokenAmount     float64 `json:"tokenAmount"`
	TokenStandard   string  `json:"tokenStandard"`
}

type heliusNativeTransfer struct {
	FromUserAccount string `json:"fromUserAccount"`
	ToUserAccount   string `json:"toUserAccount"`
	Amount          int64  `json:"amount"`
}

func (h *Helius) parseEnhanced(sigs []string, wallet string) ([]RawTransaction, error) {
	url := fmt.Sprintf("https://api.helius.xyz/v0/transactions?api-key=%s", h.APIKey)

	respBody, err := h.post(url, sigs)
	if err != nil {
		return nil, fmt.Errorf("enhanced transactions: %w", err)
	}

	var results []heliusEnhancedTx
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("parsing enhanced response: %w", err)
	}

	var txs []RawTransaction
	for _, etx := range results {
		txs = append(txs, convertHeliusTx(etx, wallet)...)
	}
	return txs, nil
}

func convertHeliusTx(etx heliusEnhancedTx, wallet string) []RawTransaction {
	walletLower := strings.ToLower(wallet)
	feeLamports := etx.Fee
	feeSOL := lamportsToSOL(feeLamports)

	switch etx.Type {
	case "SWAP":
		return convertSwap(etx, walletLower, feeSOL)
	case "TRANSFER":
		return convertTransfer(etx, walletLower, feeSOL)
	default:
		// Other types (NFT, COMPRESSED_NFT, etc.): extract relevant token movements
		return convertGeneric(etx, walletLower, feeSOL)
	}
}

func convertSwap(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	var sentAsset, rcvAsset string
	var sentAmt, rcvAmt string

	for _, tt := range etx.TokenTransfers {
		if strings.ToLower(tt.FromUserAccount) == wallet {
			sentAsset = tt.Mint
			sentAmt = formatTokenAmount(tt.TokenAmount)
		}
		if strings.ToLower(tt.ToUserAccount) == wallet {
			rcvAsset = tt.Mint
			rcvAmt = formatTokenAmount(tt.TokenAmount)
		}
	}
	for _, nt := range etx.NativeTransfers {
		if strings.ToLower(nt.FromUserAccount) == wallet && nt.Amount > 0 {
			sentAsset = "SOL"
			sentAmt = lamportsToSOL(nt.Amount)
		}
		if strings.ToLower(nt.ToUserAccount) == wallet && nt.Amount > 0 {
			rcvAsset = "SOL"
			rcvAmt = lamportsToSOL(nt.Amount)
		}
	}

	if sentAsset == "" || rcvAsset == "" {
		return nil
	}

	return []RawTransaction{{
		ID:        etx.Signature,
		Timestamp: etx.Timestamp,
		Source:    types.SourceHelius,
		Chain:     types.ChainSolana,
		Asset:     sentAsset,
		Amount:    sentAmt,
		Asset2:    rcvAsset,
		Amount2:   rcvAmt,
		Fee:       feeSOL,
		FeeAsset:  "SOL",
		RawType:   fmt.Sprintf("SWAP/%s", etx.Source),
	}}
}

func convertTransfer(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	var txs []RawTransaction

	for _, tt := range etx.TokenTransfers {
		raw := RawTransaction{
			ID:        etx.Signature,
			Timestamp: etx.Timestamp,
			Source:    types.SourceHelius,
			Chain:     types.ChainSolana,
			FromAddr:  tt.FromUserAccount,
			ToAddr:    tt.ToUserAccount,
			Asset:     tt.Mint,
			Amount:    formatTokenAmount(tt.TokenAmount),
			Fee:       feeSOL,
			FeeAsset:  "SOL",
			RawType:   "TRANSFER",
		}
		txs = append(txs, raw)
	}

	for _, nt := range etx.NativeTransfers {
		if nt.Amount == 0 {
			continue
		}
		raw := RawTransaction{
			ID:        etx.Signature,
			Timestamp: etx.Timestamp,
			Source:    types.SourceHelius,
			Chain:     types.ChainSolana,
			FromAddr:  nt.FromUserAccount,
			ToAddr:    nt.ToUserAccount,
			Asset:     "SOL",
			Amount:    lamportsToSOL(nt.Amount),
			Fee:       feeSOL,
			FeeAsset:  "SOL",
			RawType:   "TRANSFER",
		}
		txs = append(txs, raw)
	}

	return txs
}

func convertGeneric(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	var txs []RawTransaction

	for _, tt := range etx.TokenTransfers {
		if strings.ToLower(tt.FromUserAccount) != wallet && strings.ToLower(tt.ToUserAccount) != wallet {
			continue
		}
		raw := RawTransaction{
			ID:        etx.Signature,
			Timestamp: etx.Timestamp,
			Source:    types.SourceHelius,
			Chain:     types.ChainSolana,
			FromAddr:  tt.FromUserAccount,
			ToAddr:    tt.ToUserAccount,
			Asset:     tt.Mint,
			Amount:    formatTokenAmount(tt.TokenAmount),
			Fee:       feeSOL,
			FeeAsset:  "SOL",
			RawType:   etx.Type,
		}
		txs = append(txs, raw)
	}

	return txs
}

func (h *Helius) post(url string, body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := h.Client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// lamportsToSOL converts lamports (int64) to a SOL decimal string.
func lamportsToSOL(lamports int64) string {
	r := new(big.Rat).SetFrac(
		big.NewInt(lamports),
		new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil),
	)
	return r.FloatString(9)
}

func formatTokenAmount(amount float64) string {
	return new(big.Float).SetFloat64(amount).Text('f', 9)
}
