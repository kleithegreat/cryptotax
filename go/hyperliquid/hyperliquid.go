// Package hyperliquid reads a Hyperliquid account: USDC ledger movements,
// perpetual fills and funding. See docs/hyperliquid.md.
package hyperliquid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/ledger"
	"github.com/kevin/cryptotax/netcache"
)

const (
	apiURL = "https://api.hyperliquid.xyz/info"
	// The API serves only this many most-recent fills, whatever the paging.
	fillHorizon = 10000
)

type Fetcher struct{ Net *netcache.Client }

type Result struct {
	Movements []ledger.Movement
	Events    []ledger.Event
	Issues    []ledger.Issue
}

func (f *Fetcher) Read(user string) (Result, error) {
	u := strings.ToLower(user)
	account := "hyperliquid:" + u
	var res Result

	ledgerRows, err := f.paged("userNonFundingLedgerUpdates", u)
	if err != nil {
		return res, err
	}
	for _, raw := range ledgerRows {
		if err := res.ledger(raw, u, account); err != nil {
			return res, err
		}
	}

	fills, err := f.paged("userFillsByTime", u)
	if err != nil {
		return res, err
	}
	if len(fills) >= fillHorizon {
		res.Issues = append(res.Issues, ledger.Issue{Event: account, Blocking: true,
			Message: fmt.Sprintf("Hyperliquid returned %d fills, its retention limit; older fills are unrecoverable from the API", len(fills))})
	}
	for _, raw := range fills {
		if err := res.fill(raw, account); err != nil {
			return res, err
		}
	}

	funding, err := f.paged("userFunding", u)
	if err != nil {
		return res, err
	}
	for _, raw := range funding {
		var r struct {
			Time  int64 `json:"time"`
			Delta struct {
				Type string `json:"type"`
				Coin string `json:"coin"`
				USDC string `json:"usdc"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return res, err
		}
		if r.Delta.Type != "funding" {
			return res, fmt.Errorf("hyperliquid funding row of type %q", r.Delta.Type)
		}
		usd, err := decimal.Canon(r.Delta.USDC)
		if err != nil {
			return res, fmt.Errorf("hyperliquid funding: %w", err)
		}
		res.Events = append(res.Events, ledger.Event{
			ID:      fmt.Sprintf("hyperliquid:funding:%s:%s:%d", u, r.Delta.Coin, r.Time),
			Time:    time.UnixMilli(r.Time).UTC(),
			Kind:    ledger.Funding,
			Funding: &ledger.FundingPay{Account: account, Market: r.Delta.Coin, USD: usd},
		})
	}
	return res, nil
}

// ledger converts one non-funding ledger update.
func (res *Result) ledger(raw json.RawMessage, u, account string) error {
	var r struct {
		Time  int64  `json:"time"`
		Hash  string `json:"hash"`
		Delta struct {
			Type        string `json:"type"`
			USDC        string `json:"usdc"`
			Fee         string `json:"fee"`
			User        string `json:"user"`
			Destination string `json:"destination"`
			Token       string `json:"token"`
			Amount      string `json:"amount"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	d := r.Delta
	m := ledger.Movement{Time: time.UnixMilli(r.Time).UTC(), Account: account, Note: "hyperliquid " + d.Type}
	usdc := asset.HyperliquidUSDC()
	amount := func(s string) (*big.Rat, error) { return decimal.Parse(s) }

	switch d.Type {
	case "accountClassTransfer":
		return nil // between the account's own spot and perp balances
	case "deposit", "withdraw":
		// The Hyperliquid hash is the Arbitrum bridge transaction hash, so
		// the movement shares that id and pairs with the Arbitrum side.
		m.ID = "arbitrum:" + r.Hash
		m.Signed = true
		m.Counterparties = []string{"hyperliquid-bridge"}
		v, err := amount(d.USDC)
		if err != nil {
			return fmt.Errorf("hyperliquid %s %s: %w", d.Type, r.Hash, err)
		}
		if d.Type == "withdraw" {
			v.Neg(v)
			fee, err := amount(orZero(d.Fee))
			if err != nil {
				return err
			}
			if fee.Sign() != 0 {
				m.Fee = &ledger.Delta{Asset: usdc, Amount: fee}
			}
		}
		m.Deltas = []ledger.Delta{{Asset: usdc, Amount: v}}
	case "send", "spotTransfer", "internalTransfer":
		token, qty := d.Token, d.Amount
		if d.Type == "internalTransfer" {
			token, qty = "USDC", d.USDC
		}
		from, to := strings.ToLower(d.User), strings.ToLower(d.Destination)
		if from == u && to == u {
			return nil // between the account's own dexes
		}
		if token != "USDC" {
			res.Issues = append(res.Issues, ledger.Issue{Event: "hyperliquid:" + r.Hash, Blocking: true,
				Message: fmt.Sprintf("Hyperliquid %s of token %q is not supported", d.Type, token)})
			return nil
		}
		v, err := amount(qty)
		if err != nil {
			return fmt.Errorf("hyperliquid %s %s: %w", d.Type, r.Hash, err)
		}
		m.ID = "hyperliquid:" + r.Hash
		fee, err := amount(orZero(d.Fee))
		if err != nil {
			return err
		}
		switch u {
		case from:
			m.Signed = true
			m.Counterparties = []string{to}
			v.Neg(v)
			// A send's fee is charged on top; an internalTransfer's fee comes
			// out of what the recipient gets (verified by balance replay).
			if fee.Sign() != 0 && d.Type != "internalTransfer" {
				m.Fee = &ledger.Delta{Asset: usdc, Amount: fee}
			}
		case to:
			m.Counterparties = []string{from}
			if d.Type == "internalTransfer" {
				v.Sub(v, fee)
			}
		default:
			return fmt.Errorf("hyperliquid %s %s does not involve %s", d.Type, r.Hash, u)
		}
		m.Deltas = []ledger.Delta{{Asset: usdc, Amount: v}}
	default:
		res.Issues = append(res.Issues, ledger.Issue{Event: "hyperliquid:" + r.Hash, Blocking: true,
			Message: fmt.Sprintf("Hyperliquid ledger update %q at %s is not supported", d.Type, m.Time.Format(time.RFC3339))})
		return nil
	}
	res.Movements = append(res.Movements, m)
	return nil
}

// fill converts one fill. A flip fill ("Long > Short") closes the whole old
// position and opens the remainder on the other side; the exchange's
// closedPnl belongs to the closing part.
func (res *Result) fill(raw json.RawMessage, account string) error {
	var r struct {
		Coin          string `json:"coin"`
		Px            string `json:"px"`
		Sz            string `json:"sz"`
		Time          int64  `json:"time"`
		StartPosition string `json:"startPosition"`
		Dir           string `json:"dir"`
		ClosedPnl     string `json:"closedPnl"`
		Hash          string `json:"hash"`
		Fee           string `json:"fee"`
		FeeToken      string `json:"feeToken"`
		Tid           int64  `json:"tid"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	id := fmt.Sprintf("hyperliquid:fill:%d", r.Tid)
	t := time.UnixMilli(r.Time).UTC()
	issue := func(msg string) error {
		res.Issues = append(res.Issues, ledger.Issue{Event: id, Blocking: true, Message: msg})
		return nil
	}
	if r.FeeToken != "USDC" {
		return issue(fmt.Sprintf("fill fee paid in %q, only USDC is supported", r.FeeToken))
	}
	sz, err1 := decimal.Parse(r.Sz)
	fee, err2 := decimal.Parse(r.Fee)
	start, err3 := decimal.Parse(orZero(r.StartPosition))
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("fill %s: bad number", id)
	}
	pnl, err := decimal.Canon(orZero(r.ClosedPnl))
	if err != nil {
		return fmt.Errorf("fill %s: closedPnl: %w", id, err)
	}

	fill := &ledger.PerpFill{Account: account, Market: r.Coin, PnL: pnl, Fee: decimal.String(fee)}
	dir := r.Dir
	if i := strings.LastIndex(dir, " "); strings.HasPrefix(dir, "Liquidated") && i > 0 {
		dir = "Close" + dir[i:] // "Liquidated Isolated Long" closes a long
	}
	size := decimal.String(sz)
	switch dir {
	case "Open Long":
		fill.Open = &ledger.PerpPart{Side: "long", Size: size}
	case "Open Short":
		fill.Open = &ledger.PerpPart{Side: "short", Size: size}
	case "Close Long":
		fill.Close = &ledger.PerpPart{Side: "long", Size: size}
	case "Close Short":
		fill.Close = &ledger.PerpPart{Side: "short", Size: size}
	case "Long > Short", "Short > Long":
		old := new(big.Rat).Abs(start)
		rest := new(big.Rat).Sub(sz, old)
		if rest.Sign() < 0 {
			return issue(fmt.Sprintf("flip fill %q smaller than its starting position", r.Dir))
		}
		from, to := "long", "short"
		if dir == "Short > Long" {
			from, to = "short", "long"
		}
		fill.Close = &ledger.PerpPart{Side: from, Size: decimal.String(old)}
		if rest.Sign() > 0 {
			fill.Open = &ledger.PerpPart{Side: to, Size: decimal.String(rest)}
		}
	default:
		return issue(fmt.Sprintf("fill direction %q (%s) is not supported", r.Dir, r.Coin))
	}
	res.Events = append(res.Events, ledger.Event{
		ID: id, Time: t, Kind: ledger.Perp, Perp: fill,
		Note: r.Dir + " @ " + r.Px + " tx " + r.Hash,
	})
	return nil
}

// paged walks a time-windowed info endpoint. Each request starts at the
// last row's timestamp (rows sharing a millisecond may straddle pages) and
// duplicates are dropped by content; paging stops when a page adds nothing.
func (f *Fetcher) paged(kind, user string) ([]json.RawMessage, error) {
	var out []json.RawMessage
	seen := map[string]bool{}
	start := int64(0)
	for {
		body, _ := json.Marshal(map[string]any{"type": kind, "user": user, "startTime": start})
		b, err := f.Net.Fetch(netcache.Request{Key: fmt.Sprintf("hyperliquid/%s/%s/%d", user, kind, start), URL: apiURL, Body: body})
		if err != nil {
			return nil, err
		}
		var page []json.RawMessage
		if err := json.Unmarshal(b, &page); err != nil {
			return nil, fmt.Errorf("hyperliquid %s: %w", kind, err)
		}
		fresh := 0
		var last int64
		for _, raw := range page {
			var c bytes.Buffer
			_ = json.Compact(&c, raw)
			if !seen[c.String()] {
				seen[c.String()] = true
				out = append(out, raw)
				fresh++
			}
			var t struct {
				Time int64 `json:"time"`
			}
			_ = json.Unmarshal(raw, &t)
			last = max(last, t.Time)
		}
		if fresh == 0 {
			sort.SliceStable(out, func(i, j int) bool { return timeOf(out[i]) < timeOf(out[j]) })
			return out, nil
		}
		start = last
	}
}

func timeOf(raw json.RawMessage) int64 {
	var t struct {
		Time int64 `json:"time"`
	}
	_ = json.Unmarshal(raw, &t)
	return t.Time
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}
