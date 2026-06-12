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
	hlAPIURL = "https://api.hyperliquid.xyz/info"
	// Per-response caps differ per endpoint: userFillsByTime returns at most
	// 2000 fills, userFunding at most 500 entries. Checking the wrong cap
	// silently truncates history (funding once stopped after 500 rows).
	hlFillPageSize    = 2000
	hlFundingPageSize = 500
	hlRateDelay       = 250 * time.Millisecond
)

// Hyperliquid fetches trade fills from the Hyperliquid DEX.
// No API key required — fully public and unauthenticated.
type Hyperliquid struct {
	Client *http.Client
	APIURL string
	Sleep  func(time.Duration)
}

func NewHyperliquid() *Hyperliquid {
	return &Hyperliquid{
		Client: &http.Client{Timeout: 30 * time.Second},
		APIURL: hlAPIURL,
		Sleep:  time.Sleep,
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
		Index  int    `json:"index"`
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
			EventGroupID:  hyperliquidFillGroupID(wallet, fill),
			SplitReason:   "api_fill_granularity",
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
			ID:           f.Hash,
			Timestamp:    f.Time / 1000,
			Source:       types.SourceHyperliquid,
			Chain:        types.ChainHyperliquid,
			Wallet:       wallet,
			Market:       f.Delta.Coin,
			EventGroupID: hyperliquidFundingGroupID(wallet, f),
			Asset:        f.Delta.Coin,
			Amount:       f.Delta.USDC,
			RawType:      "funding",
		})
	}

	return txs, nil
}

func hyperliquidFillGroupID(wallet string, fill hlFill) string {
	if strings.TrimSpace(fill.Hash) != "" {
		return "hyperliquid:fill:" + fill.Hash
	}
	return fmt.Sprintf("hyperliquid:fill:%s:%d:%s:%s:%s", strings.ToLower(wallet), fill.Time, fill.Coin, fill.Dir, fill.Sz)
}

func hyperliquidFundingGroupID(wallet string, funding hlFunding) string {
	trimmedHash := strings.TrimSpace(funding.Hash)
	if trimmedHash != "" && trimmedHash != "0x0000000000000000000000000000000000000000000000000000000000000000" {
		return "hyperliquid:funding:" + trimmedHash
	}
	return fmt.Sprintf("hyperliquid:funding:%s:%d:%s:%s", strings.ToLower(wallet), funding.Time, funding.Delta.Coin, funding.Delta.USDC)
}

// fetchFills pages through userFillsByTime. Note the API only serves the
// 10,000 most recent fills; deeper history is unreachable regardless of
// pagination (see docs/hyperliquid/QUIRKS.md).
func (h *Hyperliquid) fetchFills(wallet string) ([]hlFill, error) {
	var allFills []hlFill
	seen := make(map[string]struct{})

	err := h.paginate("userFillsByTime", wallet, hlFillPageSize, func(respBody []byte) (int, int64, error) {
		var fills []hlFill
		if err := json.Unmarshal(respBody, &fills); err != nil {
			return 0, 0, fmt.Errorf("parsing fills: %w", err)
		}
		fresh := 0
		for _, fill := range fills {
			// Partial fills share a hash; the full tuple identifies a fill.
			key := fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s", fill.Hash, fill.Time, fill.Coin, fill.Dir, fill.Sz, fill.Px, fill.StartPosition)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			allFills = append(allFills, fill)
			fresh++
		}
		var last int64
		if len(fills) > 0 {
			last = fills[len(fills)-1].Time
		}
		return countWithFresh(len(fills), fresh), last, nil
	})
	if err != nil {
		return nil, err
	}
	return allFills, nil
}

func (h *Hyperliquid) fetchFunding(wallet string) ([]hlFunding, error) {
	var allFunding []hlFunding
	seen := make(map[string]struct{})

	err := h.paginate("userFunding", wallet, hlFundingPageSize, func(respBody []byte) (int, int64, error) {
		var funding []hlFunding
		if err := json.Unmarshal(respBody, &funding); err != nil {
			return 0, 0, fmt.Errorf("parsing funding: %w", err)
		}
		fresh := 0
		for _, f := range funding {
			// Funding hashes are commonly all-zero; key on the payload.
			key := fmt.Sprintf("%d|%s|%s|%s", f.Time, f.Delta.Coin, f.Delta.USDC, f.Delta.Type)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			allFunding = append(allFunding, f)
			fresh++
		}
		var last int64
		if len(funding) > 0 {
			last = funding[len(funding)-1].Time
		}
		return countWithFresh(len(funding), fresh), last, nil
	})
	if err != nil {
		return nil, err
	}
	return allFunding, nil
}

// countWithFresh encodes (page length, new records) for paginate.
func countWithFresh(pageLen, fresh int) int {
	if fresh == 0 {
		return -pageLen // signal: full page but nothing new → stop
	}
	return pageLen
}

// paginate walks a time-windowed Hyperliquid endpoint in ascending order.
// The next window starts AT the last record's timestamp (not +1, which
// would skip records sharing that millisecond across a page boundary);
// the per-record dedupe in the page handler absorbs the overlap.
func (h *Hyperliquid) paginate(infoType, wallet string, pageSize int, handlePage func([]byte) (int, int64, error)) error {
	startTime := int64(0)

	for {
		body := map[string]interface{}{
			"type":      infoType,
			"user":      wallet,
			"startTime": startTime,
		}

		respBody, err := h.post(body)
		if err != nil {
			return fmt.Errorf("%s: %w", infoType, err)
		}

		count, lastTime, err := handlePage(respBody)
		if err != nil {
			return err
		}
		if count < 0 || count < pageSize {
			// Short page (history exhausted) or a full page of duplicates
			// (every record at one timestamp — cannot advance further).
			return nil
		}

		startTime = lastTime
		if h.Sleep != nil {
			h.Sleep(hlRateDelay)
		}
	}
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

	// Spot fills reference pairs as "@<index>" using the universe entry's
	// explicit index field — NOT its position in the returned slice.
	spotMap := make(map[string]string)
	for _, u := range meta.Universe {
		key := fmt.Sprintf("@%d", u.Index)
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

	apiURL := h.APIURL
	if apiURL == "" {
		apiURL = hlAPIURL
	}
	resp, err := h.Client.Post(apiURL, "application/json", bytes.NewReader(jsonBody))
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
