package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newHLServer fakes the Hyperliquid info endpoint. Handlers receive the
// decoded request body and return the JSON response.
func newHLServer(t *testing.T, handle func(reqType string, startTime int64) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			Type      string `json:"type"`
			StartTime int64  `json:"startTime"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := json.Marshal(handle(req.Type, req.StartTime))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
}

func testHyperliquid(serverURL string) *Hyperliquid {
	h := NewHyperliquid()
	h.APIURL = serverURL
	h.Sleep = nil
	return h
}

func fundingEntry(timeMS int64, coin, usdc string) hlFunding {
	var f hlFunding
	f.Time = timeMS
	f.Hash = "0x0000000000000000000000000000000000000000000000000000000000000000"
	f.Delta.Coin = coin
	f.Delta.USDC = usdc
	f.Delta.Type = "funding"
	return f
}

// Funding pagination must use the 500-entry cap of userFunding, not the
// 2000-fill cap — the original bug silently truncated funding history to
// the first 500 records.
func TestFetchFundingPaginatesPast500(t *testing.T) {
	pages := map[int64][]hlFunding{}
	var first []hlFunding
	for i := 0; i < 500; i++ {
		first = append(first, fundingEntry(int64(1000+i), "ETH", fmt.Sprintf("0.%03d", i)))
	}
	pages[0] = first
	// Second window starts AT the last timestamp (1499) and overlaps it.
	pages[1499] = []hlFunding{
		fundingEntry(1499, "ETH", "0.499"), // duplicate of page-1 tail
		fundingEntry(1500, "BTC", "1.5"),
		fundingEntry(1501, "BTC", "-0.25"),
	}

	srv := newHLServer(t, func(reqType string, startTime int64) any {
		if reqType != "userFunding" {
			return []any{}
		}
		return pages[startTime]
	})
	defer srv.Close()

	funding, err := testHyperliquid(srv.URL).fetchFunding("0xabc")
	if err != nil {
		t.Fatalf("fetchFunding: %v", err)
	}
	if len(funding) != 502 {
		t.Fatalf("got %d funding records, want 502 (500 + 2 new, overlap deduped)", len(funding))
	}
	last := funding[len(funding)-1]
	if last.Delta.USDC != "-0.25" {
		t.Errorf("last funding record = %+v, want the 1501ms entry", last)
	}
}

// Records sharing the boundary millisecond must not be skipped: the next
// window starts AT the last timestamp and duplicates are deduped.
func TestFetchFillsKeepsBoundaryMillisecondSiblings(t *testing.T) {
	mkFill := func(timeMS int64, sz string) hlFill {
		return hlFill{Coin: "BTC", Px: "50000", Sz: sz, Time: timeMS, Dir: "Open Long", Hash: fmt.Sprintf("0xh%d%s", timeMS, sz)}
	}
	var first []hlFill
	for i := 0; i < hlFillPageSize-2; i++ {
		first = append(first, mkFill(int64(i), "1"))
	}
	// Page boundary lands inside the 9000ms group.
	first = append(first, mkFill(9000, "0.1"), mkFill(9000, "0.2"))

	second := []hlFill{mkFill(9000, "0.1"), mkFill(9000, "0.2"), mkFill(9000, "0.3"), mkFill(9001, "0.4")}

	srv := newHLServer(t, func(reqType string, startTime int64) any {
		if reqType != "userFillsByTime" {
			return []any{}
		}
		if startTime == 0 {
			return first
		}
		if startTime == 9000 {
			return second
		}
		return []hlFill{}
	})
	defer srv.Close()

	fills, err := testHyperliquid(srv.URL).fetchFills("0xabc")
	if err != nil {
		t.Fatalf("fetchFills: %v", err)
	}
	if len(fills) != hlFillPageSize+2 {
		t.Fatalf("got %d fills, want %d (boundary siblings kept, duplicates dropped)", len(fills), hlFillPageSize+2)
	}
}

// The @N spot mapping must use the universe entry's explicit index field,
// not its slice position.
func TestSpotMetaUsesExplicitIndex(t *testing.T) {
	srv := newHLServer(t, func(reqType string, startTime int64) any {
		if reqType != "spotMeta" {
			return []any{}
		}
		return map[string]any{
			"universe": []map[string]any{
				// Returned out of order and sparse on purpose.
				{"name": "@7", "index": 7, "tokens": []int{2, 0}},
				{"name": "@3", "index": 3, "tokens": []int{1, 0}},
			},
			"tokens": []map[string]any{
				{"name": "USDC", "index": 0},
				{"name": "HYPE", "index": 1},
				{"name": "PURR", "index": 2},
			},
		}
	})
	defer srv.Close()

	spotMap, err := testHyperliquid(srv.URL).fetchSpotMeta()
	if err != nil {
		t.Fatalf("fetchSpotMeta: %v", err)
	}
	if spotMap["@3"] != "HYPE" || spotMap["@7"] != "PURR" {
		t.Errorf("spotMap = %v, want @3→HYPE and @7→PURR", spotMap)
	}
	if _, ok := spotMap["@0"]; ok {
		t.Errorf("spotMap has positional key @0: %v", spotMap)
	}
}
