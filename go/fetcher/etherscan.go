package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
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
}

func NewEtherscan(apiKey string, chainID int, chain types.Chain) *Etherscan {
	return &Etherscan{
		APIKey:  apiKey,
		ChainID: chainID,
		Chain:   chain,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *Etherscan) Name() string {
	return fmt.Sprintf("etherscan/%s", e.Chain)
}

type etherscanResp struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Result  []etherscanTx `json:"result"`
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
	url := fmt.Sprintf(
		"https://api.etherscan.io/v2/api?chainid=%d&module=account&action=%s&address=%s&startblock=0&endblock=99999999&sort=asc&apikey=%s",
		e.ChainID, action, wallet, e.APIKey,
	)

	resp, err := e.Client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var result etherscanResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	if result.Status != "1" {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}

	var txs []RawTransaction
	for _, tx := range result.Result {
		if tx.IsError == "1" {
			continue
		}

		timestamp, err := strconv.ParseInt(tx.TimeStamp, 10, 64)
		if err != nil {
			continue
		}

		raw := RawTransaction{
			ID:        tx.Hash,
			Timestamp: timestamp,
			Source:    types.SourceEtherscan,
			Chain:     e.Chain,
			FromAddr:  tx.From,
			ToAddr:    tx.To,
			FeeAsset:  "ETH",
			RawType:   tx.FunctionName,
		}

		// Gas fee = gasUsed * gasPrice (in wei), converted to ether
		raw.Fee = weiToEther(multiplyBigInts(tx.GasUsed, tx.GasPrice))

		if action == "tokentx" {
			// ERC-20: use token symbol and convert value using token decimals
			raw.Asset = tx.TokenSymbol
			raw.Amount = tokenToDecimal(tx.Value, tx.TokenDecimal)
		} else {
			// Normal ETH tx: convert value from wei to ether
			raw.Asset = "ETH"
			raw.Amount = weiToEther(tx.Value)
		}

		txs = append(txs, raw)
	}

	return txs, nil
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
