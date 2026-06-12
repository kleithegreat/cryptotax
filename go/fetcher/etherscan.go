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

	"github.com/kevin/cryptotax/decimal"
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
	TokenSymbol     string `json:"tokenSymbol"`
	TokenDecimal    string `json:"tokenDecimal"`
	ContractAddress string `json:"contractAddress"`
}

const (
	etherscanPageSize           = 1000
	etherscanRateLimitBackoff   = time.Second
	etherscanRateLimitMaxRetrys = 4
)

func (e *Etherscan) Fetch(wallet string) ([]RawTransaction, error) {
	// Fetch normal transactions
	normal, err := e.fetchEndpoint(wallet, "txlist")
	if err != nil {
		return nil, fmt.Errorf("fetching normal txs: %w", err)
	}

	// Fetch ERC-20 token transfers
	tokens, err := e.fetchEndpoint(wallet, "tokentx")
	if err != nil {
		return nil, fmt.Errorf("fetching token txs: %w", err)
	}

	return assembleRows(wallet, normal, tokens), nil
}

// assembleRows merges txlist and tokentx rows for one wallet and fixes up
// gas-fee attribution, which the raw endpoints cannot express correctly:
//
//   - Gas is paid once per transaction, by the outer tx sender. The wallet
//     paid it only if it initiated the tx, which is visible as a txlist row
//     with from == wallet. Token rows in transactions initiated by someone
//     else (e.g. an approved spender pulling tokens) carry no fee.
//   - Within a wallet-initiated transaction the fee is attached to exactly
//     one row, preferring a row with a nonzero amount so the fee survives
//     downstream (zero-amount disposals are no-ops in the core).
//   - A zero-value txlist row is an artifact of a contract call; when token
//     rows exist for the same hash they carry the economics, so the artifact
//     row is dropped after its fee migrates. Without token rows (e.g. a bare
//     approve) the artifact row is kept so the gas evidence stays visible.
func assembleRows(wallet string, normal, tokens []RawTransaction) []RawTransaction {
	walletLower := strings.ToLower(wallet)

	initiated := make(map[string]bool)
	for _, tx := range normal {
		if strings.ToLower(tx.FromAddr) == walletLower {
			initiated[tx.ID] = true
		}
	}
	tokenCount := make(map[string]int)
	for _, tx := range tokens {
		tokenCount[tx.ID]++
	}

	var rows []RawTransaction
	for _, tx := range normal {
		if initiated[tx.ID] && isZeroAmount(tx.Amount) && tokenCount[tx.ID] > 0 {
			continue
		}
		rows = append(rows, tx)
	}
	rows = append(rows, tokens...)

	byHash := make(map[string][]int)
	for i := range rows {
		byHash[rows[i].ID] = append(byHash[rows[i].ID], i)
	}
	for hash, idxs := range byHash {
		holder := -1
		if initiated[hash] {
			for _, i := range idxs { // prefer a nonzero outbound row
				if rows[i].Fee != "" && strings.ToLower(rows[i].FromAddr) == walletLower && !isZeroAmount(rows[i].Amount) {
					holder = i
					break
				}
			}
			if holder < 0 { // fall back to any row carrying fee data
				for _, i := range idxs {
					if rows[i].Fee != "" {
						holder = i
						break
					}
				}
			}
		}
		for _, i := range idxs {
			if i != holder {
				rows[i].Fee = ""
			}
		}
	}

	return rows
}

func isZeroAmount(amount string) bool {
	sign, err := decimal.Sign(amount)
	return err == nil && sign == 0
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
	// No endblock: it defaults to the chain head. A fixed number would be a
	// silent-truncation trap on chains like Arbitrum whose block numbers
	// already exceed older hardcoded caps.
	query.Set("sort", "asc")
	query.Set("page", strconv.Itoa(page))
	query.Set("offset", strconv.Itoa(etherscanPageSize))
	query.Set("apikey", e.APIKey)
	endpoint.RawQuery = query.Encode()

	for attempt := 0; ; attempt++ {
		resp, err := e.Client.Get(endpoint.String())
		if err != nil {
			// err may embed the full request URL (including the API key)
			// via url.Error, so redact before surfacing.
			return nil, false, fmt.Errorf("%s %s request failed: %s", e.Chain, action, e.redact(err.Error()))
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
				// Reverted transactions move no value. Their gas was still
				// spent, but modeling gas on failed personal txs is an open
				// tax-semantics question, so they are skipped (conservative:
				// never overstates deductions). See docs/evm/QUIRKS.md.
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
			// Rows without gas data simply carry no fee evidence; rows with
			// unparseable gas data are a contract violation and fail loudly.
			if tx.GasUsed != "" && tx.GasPrice != "" {
				feeWei, err := multiplyBigInts(tx.GasUsed, tx.GasPrice)
				if err != nil {
					return nil, false, fmt.Errorf("%s %s tx %s gas: %w", e.Chain, action, tx.Hash, err)
				}
				raw.Fee, err = weiToEther(feeWei)
				if err != nil {
					return nil, false, fmt.Errorf("%s %s tx %s gas: %w", e.Chain, action, tx.Hash, err)
				}
			}

			if action == "tokentx" {
				raw.Asset = tx.TokenSymbol
				raw.AssetCanonical = strings.ToLower(tx.ContractAddress)
				raw.Amount, err = tokenToDecimal(tx.Value, tx.TokenDecimal)
				if err != nil {
					return nil, false, fmt.Errorf("%s %s tx %s token amount: %w", e.Chain, action, tx.Hash, err)
				}
			} else {
				raw.Asset = "ETH"
				raw.Amount, err = weiToEther(tx.Value)
				if err != nil {
					return nil, false, fmt.Errorf("%s %s tx %s value: %w", e.Chain, action, tx.Hash, err)
				}
			}

			txs = append(txs, raw)
		}

		return txs, exhausted || len(result) < etherscanPageSize, nil
	}
}

// redact removes the API key from text destined for errors or logs.
func (e *Etherscan) redact(text string) string {
	if e.APIKey == "" {
		return text
	}
	return strings.ReplaceAll(text, e.APIKey, "***")
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

// maxTokenDecimals bounds the 10^d divisor so hostile token metadata cannot
// drive unbounded big.Int exponentiation. No real ERC-20 exceeds this.
const maxTokenDecimals = 78

// weiToEther converts a wei string to ether (divide by 1e18).
func weiToEther(wei string) (string, error) {
	w, ok := new(big.Int).SetString(wei, 10)
	if !ok {
		return "", fmt.Errorf("invalid wei amount %q", wei)
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	return decimal.String(new(big.Rat).SetFrac(w, divisor)), nil
}

// tokenToDecimal converts a raw token amount using the token's decimal places.
func tokenToDecimal(value, decimals string) (string, error) {
	v, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return "", fmt.Errorf("invalid token amount %q", value)
	}
	d, err := strconv.Atoi(decimals)
	if err != nil || d < 0 || d > maxTokenDecimals {
		return "", fmt.Errorf("invalid token decimals %q", decimals)
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	return decimal.String(new(big.Rat).SetFrac(v, divisor)), nil
}

// multiplyBigInts multiplies two integer strings.
func multiplyBigInts(a, b string) (string, error) {
	bigA, ok1 := new(big.Int).SetString(a, 10)
	bigB, ok2 := new(big.Int).SetString(b, 10)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("invalid integers %q, %q", a, b)
	}
	return new(big.Int).Mul(bigA, bigB).String(), nil
}
