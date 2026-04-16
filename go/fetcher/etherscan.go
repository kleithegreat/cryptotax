package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/cryptotax/types"
)

// Etherscan fetches transaction history via the Etherscan V2 API.
// A single API key covers both Ethereum (chainid=1) and Arbitrum (chainid=42161).
type Etherscan struct {
	APIKey  string
	ChainID int
	Chain   types.Chain
	Client  *http.Client
	BaseURL string
	Sleep   func(time.Duration)
}

func NewEtherscan(apiKey string, chainID int, chain types.Chain) *Etherscan {
	return &Etherscan{
		APIKey:  apiKey,
		ChainID: chainID,
		Chain:   chain,
		Client:  &http.Client{Timeout: 30 * time.Second},
		BaseURL: "https://api.etherscan.io/v2/api",
		Sleep:   time.Sleep,
	}
}

func (e *Etherscan) Name() string {
	return fmt.Sprintf("etherscan/%s", e.Chain)
}

type etherscanResp struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

type etherscanTx struct {
	Hash         string `json:"hash"`
	TimeStamp    string `json:"timeStamp"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
	Gas          string `json:"gas"`
	GasPrice     string `json:"gasPrice"`
	GasUsed      string `json:"gasUsed"`
	FunctionName string `json:"functionName"`
	IsError      string `json:"isError"`
	// Token transfer fields (only populated for tokentx action)
	TokenSymbol  string `json:"tokenSymbol"`
	TokenDecimal string `json:"tokenDecimal"`
}

const (
	etherscanPageSize           = 1000
	etherscanRateLimitBackoff   = time.Second
	etherscanRateLimitMaxRetrys = 4
)

func (e *Etherscan) Fetch(wallet string) ([]RawTransaction, error) {
	var allTxs []RawTransaction

	// Fetch normal transactions
	normal, err := e.fetchEndpoint(wallet, "txlist")
	if err != nil {
		return nil, fmt.Errorf("fetching normal txs: %w", err)
	}
	allTxs = append(allTxs, normal...)

	// Fetch ERC-20 token transfers
	tokens, err := e.fetchEndpoint(wallet, "tokentx")
	if err != nil {
		return nil, fmt.Errorf("fetching token txs: %w", err)
	}
	allTxs = append(allTxs, tokens...)

	return allTxs, nil
}

func (e *Etherscan) fetchEndpoint(wallet, action string) ([]RawTransaction, error) {
	var allTxs []RawTransaction

	for page := 1; ; page++ {
		pageTxs, exhausted, err := e.fetchPage(wallet, action, page)
		if err != nil {
			return nil, err
		}
		allTxs = append(allTxs, pageTxs...)
		if exhausted {
			break
		}
	}

	return allTxs, nil
}

func (e *Etherscan) fetchPage(wallet, action string, page int) ([]RawTransaction, bool, error) {
	endpoint, err := url.Parse(e.BaseURL)
	if err != nil {
		return nil, false, fmt.Errorf("invalid Etherscan base URL %q: %w", e.BaseURL, err)
	}

	query := endpoint.Query()
	query.Set("chainid", strconv.Itoa(e.ChainID))
	query.Set("module", "account")
	query.Set("action", action)
	query.Set("address", wallet)
	query.Set("startblock", "0")
	query.Set("endblock", "99999999")
	query.Set("sort", "asc")
	query.Set("page", strconv.Itoa(page))
	query.Set("offset", strconv.Itoa(etherscanPageSize))
	query.Set("apikey", e.APIKey)
	endpoint.RawQuery = query.Encode()

	for attempt := 0; ; attempt++ {
		resp, err := e.Client.Get(endpoint.String())
		if err != nil {
			return nil, false, fmt.Errorf("%s %s request failed: %w", e.Chain, action, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, false, fmt.Errorf("%s %s response read failed: %w", e.Chain, action, readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < etherscanRateLimitMaxRetrys {
			e.sleep(etherscanRetryDelay(attempt))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf(
				"%s %s HTTP error: status_code=%d body=%s",
				e.Chain,
				action,
				resp.StatusCode,
				compactJSONSnippet(body),
			)
		}

		var envelope etherscanResp
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, false, fmt.Errorf("%s %s JSON parse failed: %w", e.Chain, action, err)
		}

		result, exhausted, err := parseEtherscanResult(envelope)
		if err != nil {
			if isEtherscanRateLimitError(err) && attempt < etherscanRateLimitMaxRetrys {
				e.sleep(etherscanRetryDelay(attempt))
				continue
			}
			return nil, false, fmt.Errorf("%s %s API error: %w", e.Chain, action, err)
		}

		var txs []RawTransaction
		for _, tx := range result {
			if tx.IsError == "1" {
				continue
			}

			timestamp, err := strconv.ParseInt(tx.TimeStamp, 10, 64)
			if err != nil {
				return nil, false, fmt.Errorf("%s %s invalid timestamp %q for tx %s", e.Chain, action, tx.TimeStamp, tx.Hash)
			}

			raw := RawTransaction{
				ID:           tx.Hash,
				Timestamp:    timestamp,
				Source:       types.SourceEtherscan,
				Chain:        e.Chain,
				Wallet:       wallet,
				EventGroupID: tx.Hash,
				SplitReason:  "source_transfer_granularity",
				FromAddr:     tx.From,
				ToAddr:       tx.To,
				FeeAsset:     "ETH",
				RawType:      tx.FunctionName,
			}

			// Gas fee = gasUsed * gasPrice (in wei), converted to ether.
			raw.Fee = weiToEther(multiplyBigInts(tx.GasUsed, tx.GasPrice))

			if action == "tokentx" {
				raw.Asset = tx.TokenSymbol
				raw.Amount = tokenToDecimal(tx.Value, tx.TokenDecimal)
			} else {
				raw.Asset = "ETH"
				raw.Amount = weiToEther(tx.Value)
			}

			txs = append(txs, raw)
		}

		return txs, exhausted || len(result) < etherscanPageSize, nil
	}
}

func (e *Etherscan) sleep(delay time.Duration) {
	if e.Sleep != nil {
		e.Sleep(delay)
		return
	}
	time.Sleep(delay)
}

func etherscanRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt+1) * etherscanRateLimitBackoff
}

func isEtherscanRateLimitError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "rate limit") ||
		strings.Contains(message, "max calls per sec") ||
		strings.Contains(message, "too many requests")
}

func parseEtherscanResult(envelope etherscanResp) ([]etherscanTx, bool, error) {
	trimmed := strings.TrimSpace(string(envelope.Result))
	if trimmed == "" || trimmed == "null" {
		if envelope.Status == "1" {
			return nil, true, nil
		}
		return nil, true, fmt.Errorf(
			"status=%q message=%q result=%q",
			envelope.Status,
			envelope.Message,
			"",
		)
	}

	if strings.HasPrefix(trimmed, "\"") {
		var resultText string
		if err := json.Unmarshal(envelope.Result, &resultText); err != nil {
			return nil, true, fmt.Errorf("status=%q message=%q result=%s", envelope.Status, envelope.Message, trimmed)
		}
		if isEtherscanEmptyResult(envelope.Message, resultText) {
			return nil, true, nil
		}
		return nil, true, fmt.Errorf(
			"status=%q message=%q result=%q",
			envelope.Status,
			envelope.Message,
			resultText,
		)
	}

	var txs []etherscanTx
	if err := json.Unmarshal(envelope.Result, &txs); err != nil {
		return nil, false, fmt.Errorf(
			"status=%q message=%q result=%s parse_error=%v",
			envelope.Status,
			envelope.Message,
			compactJSONSnippet(envelope.Result),
			err,
		)
	}

	if envelope.Status != "1" && !isEtherscanEmptyResult(envelope.Message, trimmed) {
		return nil, false, fmt.Errorf(
			"status=%q message=%q result=%s",
			envelope.Status,
			envelope.Message,
			compactJSONSnippet(envelope.Result),
		)
	}

	return txs, len(txs) == 0, nil
}

func isEtherscanEmptyResult(message, result string) bool {
	combined := strings.ToLower(strings.TrimSpace(message + " " + result))
	return strings.Contains(combined, "no transactions found") || strings.Contains(combined, "no records found")
}

func compactJSONSnippet(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) <= 240 {
		return trimmed
	}
	return trimmed[:240] + "..."
}

// weiToEther converts a wei string to ether (divide by 1e18).
func weiToEther(wei string) string {
	w, ok := new(big.Int).SetString(wei, 10)
	if !ok {
		return "0"
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	ether := new(big.Rat).SetFrac(w, divisor)
	return ether.FloatString(18)
}

// tokenToDecimal converts a raw token amount using the token's decimal places.
func tokenToDecimal(value, decimals string) string {
	v, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return "0"
	}
	d, err := strconv.Atoi(decimals)
	if err != nil || d < 0 {
		return "0"
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	result := new(big.Rat).SetFrac(v, divisor)
	return result.FloatString(d)
}

// multiplyBigInts multiplies two integer strings.
func multiplyBigInts(a, b string) string {
	bigA, ok1 := new(big.Int).SetString(a, 10)
	bigB, ok2 := new(big.Int).SetString(b, 10)
	if !ok1 || !ok2 {
		return "0"
	}
	return new(big.Int).Mul(bigA, bigB).String()
}
