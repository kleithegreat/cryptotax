// Package evm derives exact per-transaction balance movements of an EVM
// address from Etherscan v2 account endpoints. All rows of one transaction
// (normal, internal, ERC-20, NFT) are netted into a single movement.
package evm

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/ledger"
	"github.com/kevin/cryptotax/netcache"
)

// ChainIDs lists supported chains. Only chains whose gas coin is ETH are
// supported; adding one with another gas coin needs an asset.Native entry.
var ChainIDs = map[string]int{
	"ethereum": 1,
	"arbitrum": 42161,
}

const pageSize = 1000

type Fetcher struct {
	Net    *netcache.Client
	APIKey string
}

type row struct {
	Hash            string `json:"hash"`
	TimeStamp       string `json:"timeStamp"`
	BlockNumber     string `json:"blockNumber"`
	From            string `json:"from"`
	To              string `json:"to"`
	Value           string `json:"value"`
	IsError         string `json:"isError"`
	GasUsed         string `json:"gasUsed"`
	GasPrice        string `json:"gasPrice"`
	FunctionName    string `json:"functionName"`
	MethodID        string `json:"methodId"`
	ContractAddress string `json:"contractAddress"`
	TokenSymbol     string `json:"tokenSymbol"`
	TokenDecimal    string `json:"tokenDecimal"`
	TokenID         string `json:"tokenID"`
	TokenValue      string `json:"tokenValue"`
	LogIndex        string `json:"logIndex"`
	TraceID         string `json:"traceId"`
}

// Movements returns one movement per transaction that touched the address.
func (f *Fetcher) Movements(chain, address string) ([]ledger.Movement, error) {
	id, ok := ChainIDs[chain]
	if !ok {
		return nil, fmt.Errorf("unsupported EVM chain %q", chain)
	}
	w := strings.ToLower(address)
	rows := map[string][]row{}
	for _, action := range []string{"txlist", "txlistinternal", "tokentx", "tokennfttx", "token1155tx"} {
		rs, err := f.list(id, chain, action, w)
		if err != nil {
			return nil, err
		}
		rows[action] = rs
	}

	type acc struct {
		m      ledger.Movement
		deltas map[string]*ledger.Delta
		cps    map[string]bool
	}
	txs := map[string]*acc{}
	get := func(r row) (*acc, error) {
		if a := txs[r.Hash]; a != nil {
			return a, nil
		}
		ts, err := strconv.ParseInt(r.TimeStamp, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s tx %s: timestamp %q", chain, r.Hash, r.TimeStamp)
		}
		a := &acc{
			m:      ledger.Movement{ID: chain + ":" + r.Hash, Time: time.Unix(ts, 0).UTC(), Account: chain + ":" + w},
			deltas: map[string]*ledger.Delta{},
			cps:    map[string]bool{},
		}
		txs[r.Hash] = a
		return a, nil
	}
	move := func(a *acc, as asset.Asset, amount *big.Rat, from, to string) {
		from, to = strings.ToLower(from), strings.ToLower(to)
		if from == to || amount.Sign() == 0 {
			return
		}
		var signed *big.Rat
		switch w {
		case from:
			signed = new(big.Rat).Neg(amount)
			a.cps[to] = true
		case to:
			signed = new(big.Rat).Set(amount)
			a.cps[from] = true
		default:
			return
		}
		d := a.deltas[as.Key]
		if d == nil {
			d = &ledger.Delta{Asset: as, Amount: new(big.Rat)}
			a.deltas[as.Key] = d
		}
		d.Amount.Add(d.Amount, signed)
	}

	native := asset.Native(chain)
	for _, r := range rows["txlist"] {
		a, err := get(r)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(r.From, w) {
			a.m.Signed = true
			fee, err := weiProduct(r.GasUsed, r.GasPrice)
			if err != nil {
				return nil, fmt.Errorf("%s tx %s gas: %w", chain, r.Hash, err)
			}
			a.m.Fee = &ledger.Delta{Asset: native, Amount: fee}
			a.m.Note = method(r)
			if r.IsError == "1" {
				a.m.Note = "FAILED " + a.m.Note
			}
		}
		if r.IsError == "1" {
			continue
		}
		v, err := units(r.Value, 18)
		if err != nil {
			return nil, fmt.Errorf("%s tx %s value: %w", chain, r.Hash, err)
		}
		move(a, native, v, r.From, r.To)
	}
	for _, r := range rows["txlistinternal"] {
		if r.IsError == "1" {
			continue
		}
		a, err := get(r)
		if err != nil {
			return nil, err
		}
		v, err := units(r.Value, 18)
		if err != nil {
			return nil, fmt.Errorf("%s internal tx %s value: %w", chain, r.Hash, err)
		}
		move(a, native, v, r.From, r.To)
	}
	for _, r := range rows["tokentx"] {
		a, err := get(r)
		if err != nil {
			return nil, err
		}
		dec, err := strconv.Atoi(r.TokenDecimal)
		if err != nil || dec < 0 || dec > 77 {
			dec = 0 // malformed metadata is a spam signature; keep raw units
		}
		v, err := units(r.Value, dec)
		if err != nil {
			return nil, fmt.Errorf("%s token tx %s value: %w", chain, r.Hash, err)
		}
		move(a, asset.Token(chain, r.ContractAddress, r.TokenSymbol), v, r.From, r.To)
	}
	for _, action := range []string{"tokennfttx", "token1155tx"} {
		for _, r := range rows[action] {
			a, err := get(r)
			if err != nil {
				return nil, err
			}
			qty := "1"
			if action == "token1155tx" {
				qty = r.TokenValue
			}
			v, err := units(qty, 0)
			if err != nil {
				return nil, fmt.Errorf("%s nft tx %s value: %w", chain, r.Hash, err)
			}
			nft := asset.Token(chain, r.ContractAddress+"#"+r.TokenID, r.TokenSymbol)
			move(a, nft, v, r.From, r.To)
		}
	}

	var out []ledger.Movement
	for _, a := range txs {
		for _, d := range a.deltas {
			if d.Amount.Sign() != 0 {
				a.m.Deltas = append(a.m.Deltas, *d)
			}
		}
		sort.Slice(a.m.Deltas, func(i, j int) bool { return a.m.Deltas[i].Asset.Key < a.m.Deltas[j].Asset.Key })
		for c := range a.cps {
			a.m.Counterparties = append(a.m.Counterparties, c)
		}
		sort.Strings(a.m.Counterparties)
		if len(a.m.Deltas) > 0 || a.m.Fee != nil {
			out = append(out, a.m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// list pages through one account endpoint. Etherscan refuses page*offset
// beyond 10,000, so after each full window the next query restarts at the
// last block seen; rows of that block are deduplicated.
func (f *Fetcher) list(chainID int, chain, action, w string) ([]row, error) {
	var out []row
	seen := map[string]bool{}
	start := "0"
	for {
		var windowLast string
		full := false
		for page := 1; page <= 10; page++ {
			url := fmt.Sprintf("https://api.etherscan.io/v2/api?chainid=%d&module=account&action=%s&address=%s&startblock=%s&sort=asc&page=%d&offset=%d&apikey=%s",
				chainID, action, w, start, page, pageSize, f.APIKey)
			b, err := f.Net.Fetch(netcache.Request{Key: fmt.Sprintf("evm/%s/%s/%s/%s-%d", chain, w, action, start, page), URL: url, Retry: rateLimited})
			if err != nil {
				return nil, err
			}
			rs, err := parse(b)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", chain, action, err)
			}
			for _, r := range rs {
				k := r.Hash + "|" + r.LogIndex + "|" + r.TraceID + "|" + r.TokenID + "|" + r.From + "|" + r.To + "|" + r.Value
				if !seen[k] {
					seen[k] = true
					out = append(out, r)
				}
				windowLast = r.BlockNumber
			}
			if len(rs) < pageSize {
				return out, nil
			}
			full = page == 10
		}
		if !full || windowLast == start {
			return nil, fmt.Errorf("%s %s: more than 10,000 rows in block %s", chain, action, start)
		}
		start = windowLast
	}
}

func parse(b []byte) ([]row, error) {
	var env struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	var rs []row
	if err := json.Unmarshal(env.Result, &rs); err == nil {
		if env.Status != "1" && len(rs) > 0 {
			return nil, fmt.Errorf("status %s: %s", env.Status, env.Message)
		}
		return rs, nil
	}
	var msg string
	_ = json.Unmarshal(env.Result, &msg)
	if strings.Contains(strings.ToLower(env.Message+msg), "no transactions found") {
		return nil, nil
	}
	return nil, fmt.Errorf("etherscan: %s: %s", env.Message, msg)
}

func rateLimited(b []byte) bool {
	s := strings.ToLower(string(b))
	return strings.Contains(s, "rate limit") || strings.Contains(s, "max calls per sec")
}

func units(raw string, decimals int) (*big.Rat, error) {
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, fmt.Errorf("integer %q", raw)
	}
	return new(big.Rat).SetFrac(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)), nil
}

func weiProduct(a, b string) (*big.Rat, error) {
	x, ok1 := new(big.Int).SetString(a, 10)
	y, ok2 := new(big.Int).SetString(b, 10)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("integers %q × %q", a, b)
	}
	return new(big.Rat).SetFrac(x.Mul(x, y), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)), nil
}

func method(r row) string {
	if name, _, _ := strings.Cut(r.FunctionName, "("); name != "" {
		return name
	}
	return r.MethodID
}
