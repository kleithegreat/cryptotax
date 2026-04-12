package fetcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/types"
)

const (
	hlAPIURL       = "https://api.hyperliquid.xyz/info"
	hlFillPageSize = 2000
	hlRateDelay    = 250 * time.Millisecond
)

// Hyperliquid fetches trade fills from the Hyperliquid DEX.
// No API key required — fully public and unauthenticated.
type Hyperliquid struct {
	Client *http.Client
}

func NewHyperliquid() *Hyperliquid {
	return &Hyperliquid{
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *Hyperliquid) Name() string { return "hyperliquid" }

type hlFill struct {
	Coin          string `json:"coin"`
	Px            string `json:"px"`
	Sz            string `json:"sz"`
	Side          string `json:"side"`
	Time          int64  `json:"time"` // unix ms
	StartPosition string `json:"startPosition"`
	Dir           string `json:"dir"`
	ClosedPnl     string `json:"closedPnl"`
	Hash          string `json:"hash"`
	Fee           string `json:"fee"`
	FeeToken      string `json:"feeToken"`
}

type hlFunding struct {
	Time  int64  `json:"time"`
	Hash  string `json:"hash"`
	Delta struct {
		Coin string `json:"coin"`
		USDC string `json:"usdc"`
		Type string `json:"type"`
	} `json:"delta"`
}

type hlSpotMetaResp struct {
	Universe []struct {
		Tokens []int  `json:"tokens"`
		Name   string `json:"name"`
	} `json:"universe"`
	Tokens []struct {
		Name  string `json:"name"`
		Index int    `json:"index"`
	} `json:"tokens"`
}

func (h *Hyperliquid) Fetch(wallet string) ([]RawTransaction, error) {
	// Resolve spot @N coin mappings
	spotMap, err := h.fetchSpotMeta()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch spot metadata: %v\n", err)
		spotMap = make(map[string]string)
	}

	// Fetch all trade fills
	fills, err := h.fetchFills(wallet)
	if err != nil {
		return nil, fmt.Errorf("fetching fills: %w", err)
	}

	// Fetch funding payments
	funding, err := h.fetchFunding(wallet)
	if err != nil {
		return nil, fmt.Errorf("fetching funding: %w", err)
	}

	var txs []RawTransaction

	for _, fill := range fills {
		coin := fill.Coin
		if strings.HasPrefix(coin, "@") {
			if resolved, ok := spotMap[coin]; ok {
				coin = resolved
			}
		}

		txs = append(txs, RawTransaction{
			ID:            fill.Hash,
			Timestamp:     fill.Time / 1000, // ms → seconds
			Source:        types.SourceHyperliquid,
			Chain:         types.ChainHyperliquid,
			Wallet:        wallet,
			Asset:         coin,
			Amount:        fill.Sz,
			USDPrice:      fill.Px,
			Fee:           fill.Fee,
			FeeAsset:      fill.FeeToken,
			RawType:       fill.Dir,
			ClosedPnl:     fill.ClosedPnl,
			StartPosition: fill.StartPosition,
		})
	}

	for _, f := range funding {
		txs = append(txs, RawTransaction{
			ID:        f.Hash,
			Timestamp: f.Time / 1000,
			Source:    types.SourceHyperliquid,
			Chain:     types.ChainHyperliquid,
			Wallet:    wallet,
			Asset:     f.Delta.Coin,
			Amount:    f.Delta.USDC,
			RawType:   "funding",
		})
	}

	return txs, nil
}

func (h *Hyperliquid) fetchFills(wallet string) ([]hlFill, error) {
	var allFills []hlFill
	startTime := int64(0)

	for {
		body := map[string]interface{}{
			"type":      "userFillsByTime",
			"user":      wallet,
			"startTime": startTime,
		}

		respBody, err := h.post(body)
		if err != nil {
			return nil, fmt.Errorf("userFillsByTime: %w", err)
		}

		var fills []hlFill
		if err := json.Unmarshal(respBody, &fills); err != nil {
			return nil, fmt.Errorf("parsing fills: %w", err)
		}

		allFills = append(allFills, fills...)

		if len(fills) < hlFillPageSize {
			break
		}
		// Continue pagination from last timestamp + 1
		startTime = fills[len(fills)-1].Time + 1

		time.Sleep(hlRateDelay)
	}

	return allFills, nil
}

func (h *Hyperliquid) fetchFunding(wallet string) ([]hlFunding, error) {
	var allFunding []hlFunding
	startTime := int64(0)

	for {
		body := map[string]interface{}{
			"type":      "userFunding",
			"user":      wallet,
			"startTime": startTime,
		}

		respBody, err := h.post(body)
		if err != nil {
			return nil, fmt.Errorf("userFunding: %w", err)
		}

		var funding []hlFunding
		if err := json.Unmarshal(respBody, &funding); err != nil {
			return nil, fmt.Errorf("parsing funding: %w", err)
		}

		allFunding = append(allFunding, funding...)

		if len(funding) < hlFillPageSize {
			break
		}
		startTime = funding[len(funding)-1].Time + 1

		time.Sleep(hlRateDelay)
	}

	return allFunding, nil
}

func (h *Hyperliquid) fetchSpotMeta() (map[string]string, error) {
	body := map[string]interface{}{
		"type": "spotMeta",
	}

	respBody, err := h.post(body)
	if err != nil {
		return nil, err
	}

	var meta hlSpotMetaResp
	if err := json.Unmarshal(respBody, &meta); err != nil {
		return nil, fmt.Errorf("parsing spotMeta: %w", err)
	}

	// Build @N → token name mapping
	tokenNames := make(map[int]string)
	for _, t := range meta.Tokens {
		tokenNames[t.Index] = t.Name
	}

	spotMap := make(map[string]string)
	for i, u := range meta.Universe {
		key := fmt.Sprintf("@%d", i)
		if len(u.Tokens) > 0 {
			if name, ok := tokenNames[u.Tokens[0]]; ok {
				spotMap[key] = name
			}
		}
	}

	return spotMap, nil
}

func (h *Hyperliquid) post(body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := h.Client.Post(hlAPIURL, "application/json", bytes.NewReader(jsonBody))
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
