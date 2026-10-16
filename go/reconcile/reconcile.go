// Package reconcile replays every movement and compares the result with the
// live balance each source reports today. A mismatch means history is
// missing (or invented), so every basis derived from it is suspect.
package reconcile

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/config"
	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/evm"
	"github.com/kevin/cryptotax/ledger"
	"github.com/kevin/cryptotax/netcache"
)

// Hyperliquid's API rounds and settles in ways that leave cent-level noise.
var hlTolerance = big.NewRat(1, 1)

type balances map[string]map[string]*big.Rat // account -> asset key -> amount

func (b balances) add(account, key string, v *big.Rat) {
	if b[account] == nil {
		b[account] = map[string]*big.Rat{}
	}
	if b[account][key] == nil {
		b[account][key] = new(big.Rat)
	}
	b[account][key].Add(b[account][key], v)
}

func Check(cfg *config.Config, net *netcache.Client, ms []ledger.Movement, direct []ledger.Event) ([]ledger.Issue, error) {
	expected := balances{}
	signedKeys := map[string]map[string]bool{}
	for _, m := range ms {
		for _, d := range m.Deltas {
			expected.add(m.Account, d.Asset.Key, d.Amount)
			if m.Signed {
				if signedKeys[m.Account] == nil {
					signedKeys[m.Account] = map[string]bool{}
				}
				signedKeys[m.Account][d.Asset.Key] = true
			}
		}
		if m.Fee != nil {
			expected.add(m.Account, m.Fee.Asset.Key, new(big.Rat).Neg(m.Fee.Amount))
		}
	}
	for _, e := range direct {
		switch {
		case e.Perp != nil:
			pnl, _ := decimal.Parse(e.Perp.PnL)
			fee, _ := decimal.Parse(e.Perp.Fee)
			expected.add(e.Perp.Account, asset.Cash, pnl.Sub(pnl, fee))
		case e.Funding != nil:
			v, _ := decimal.Parse(e.Funding.USD)
			expected.add(e.Funding.Account, asset.Cash, v)
		}
	}

	var issues []ledger.Issue
	mismatch := func(account, key string, want, got *big.Rat) {
		issues = append(issues, ledger.Issue{Event: account, Blocking: true, Message: fmt.Sprintf(
			"balance of %s: history replays to %s but the source reports %s now; history is incomplete",
			key, decimal.String(want), decimal.String(got))})
	}

	etherscanKey := os.Getenv("ETHERSCAN_API_KEY")
	heliusKey := os.Getenv("HELIUS_API_KEY")
	for _, w := range cfg.Wallets.EVM {
		for _, chain := range cfg.EVMChains {
			account := chain + ":" + strings.ToLower(w)
			keys := map[string]bool{"ETH": true, asset.Cash: true}
			for k := range signedKeys[account] {
				keys[k] = true
			}
			for _, key := range sorted(keys) {
				got, err := evmBalance(net, etherscanKey, chain, strings.ToLower(w), key)
				if err != nil {
					return nil, err
				}
				want := expected[account][key]
				if want == nil {
					want = new(big.Rat)
				}
				if want.Cmp(got) != 0 {
					mismatch(account, key, want, got)
				}
			}
		}
	}
	for _, w := range cfg.Wallets.Solana {
		account := "solana:" + w
		live, err := solanaBalances(net, heliusKey, w)
		if err != nil {
			return nil, err
		}
		keys := map[string]bool{}
		for k := range live {
			keys[k] = true
		}
		for k := range expected[account] {
			keys[k] = true
		}
		for _, key := range sorted(keys) {
			want, got := orZero(expected[account][key]), orZero(live[key])
			if want.Cmp(got) != 0 {
				mismatch(account, key, want, got)
			}
		}
	}
	for _, w := range cfg.Wallets.Hyperliquid {
		u := strings.ToLower(w)
		account := "hyperliquid:" + u
		got, err := hyperliquidCash(net, u)
		if err != nil {
			return nil, err
		}
		want := orZero(expected[account][asset.Cash])
		diff := new(big.Rat).Sub(want, got)
		if diff.Abs(diff).Cmp(hlTolerance) > 0 {
			mismatch(account, asset.Cash+" (realized + unrealized)", want, got)
		}
	}
	return issues, nil
}

func evmBalance(net *netcache.Client, key, chain, w, assetKey string) (*big.Rat, error) {
	id := evm.ChainIDs[chain]
	total := new(big.Rat)
	query := func(cacheKey, params string, decimals int) error {
		b, err := net.Fetch(netcache.Request{
			Key:   cacheKey,
			URL:   fmt.Sprintf("https://api.etherscan.io/v2/api?chainid=%d&module=account&%s&address=%s&tag=latest&apikey=%s", id, params, w, key),
			Retry: func(b []byte) bool { return strings.Contains(strings.ToLower(string(b)), "rate limit") },
		})
		if err != nil {
			return err
		}
		var r struct {
			Status, Message, Result string
		}
		if err := json.Unmarshal(b, &r); err != nil || r.Status != "1" {
			return fmt.Errorf("etherscan balance %s: %s %s", cacheKey, r.Message, r.Result)
		}
		v, ok := new(big.Int).SetString(r.Result, 10)
		if !ok {
			return fmt.Errorf("etherscan balance %s: %q", cacheKey, r.Result)
		}
		total.Add(total, new(big.Rat).SetFrac(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
		return nil
	}
	if assetKey == "ETH" {
		if err := query(fmt.Sprintf("balance/%s/%s/native", chain, w), "action=balance", 18); err != nil {
			return nil, err
		}
	}
	for _, c := range asset.Contracts(chain, assetKey) {
		dec, err := tokenDecimals(net, key, id, chain, c)
		if err != nil {
			return nil, err
		}
		if err := query(fmt.Sprintf("balance/%s/%s/%s", chain, w, c), "action=tokenbalance&contractaddress="+c, dec); err != nil {
			return nil, err
		}
	}
	return total, nil
}

// tokenDecimals reads decimals() through Etherscan's eth_call proxy.
func tokenDecimals(net *netcache.Client, key string, id int, chain, contract string) (int, error) {
	b, err := net.Fetch(netcache.Request{
		Key:       fmt.Sprintf("decimals/%s/%s", chain, contract),
		URL:       fmt.Sprintf("https://api.etherscan.io/v2/api?chainid=%d&module=proxy&action=eth_call&to=%s&data=0x313ce567&tag=latest&apikey=%s", id, contract, key),
		Immutable: true,
		Retry:     func(b []byte) bool { return strings.Contains(strings.ToLower(string(b)), "rate limit") },
	})
	if err != nil {
		return 0, err
	}
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(r.Result, "0x"), 16)
	if !ok || v.BitLen() > 8 {
		return 0, fmt.Errorf("decimals() of %s on %s: %q", contract, chain, r.Result)
	}
	return int(v.Int64()), nil
}

func solanaBalances(net *netcache.Client, key, w string) (map[string]*big.Rat, error) {
	rpc := func(cacheKey string, method string, params ...any) (json.RawMessage, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		b, err := net.Fetch(netcache.Request{Key: cacheKey, URL: "https://mainnet.helius-rpc.com/?api-key=" + key, Body: body})
		if err != nil {
			return nil, err
		}
		var r struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		return r.Result, nil
	}
	out := map[string]*big.Rat{}
	add := func(k string, v *big.Rat) {
		if out[k] == nil {
			out[k] = new(big.Rat)
		}
		out[k].Add(out[k], v)
	}
	lamports := func(n int64) *big.Rat { return new(big.Rat).SetFrac(big.NewInt(n), big.NewInt(1_000_000_000)) }

	res, err := rpc("balance/solana/"+w, "getBalance", w)
	if err != nil {
		return nil, err
	}
	var bal struct {
		Value int64 `json:"value"`
	}
	if err := json.Unmarshal(res, &bal); err != nil {
		return nil, err
	}
	add("SOL", lamports(bal.Value))

	for _, program := range []string{"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"} {
		res, err := rpc("balance/solana/"+w+"/"+program, "getTokenAccountsByOwner", w, map[string]string{"programId": program}, map[string]string{"encoding": "jsonParsed"})
		if err != nil {
			return nil, err
		}
		var accts struct {
			Value []struct {
				Account struct {
					Lamports int64 `json:"lamports"`
					Data     struct {
						Parsed struct {
							Info struct {
								Mint        string `json:"mint"`
								TokenAmount struct {
									Amount   string `json:"amount"`
									Decimals int    `json:"decimals"`
								} `json:"tokenAmount"`
							} `json:"info"`
						} `json:"parsed"`
					} `json:"data"`
				} `json:"account"`
			} `json:"value"`
		}
		if err := json.Unmarshal(res, &accts); err != nil {
			return nil, err
		}
		for _, a := range accts.Value {
			// Token-account lamports (rent, wrapped SOL) are the owner's SOL,
			// exactly as the movement derivation counts them.
			add("SOL", lamports(a.Account.Lamports))
			info := a.Account.Data.Parsed.Info
			if info.Mint == "So11111111111111111111111111111111111111112" {
				continue
			}
			raw, ok := new(big.Int).SetString(info.TokenAmount.Amount, 10)
			if !ok {
				return nil, fmt.Errorf("token amount %q", info.TokenAmount.Amount)
			}
			add(asset.Token("solana", info.Mint, "").Key,
				new(big.Rat).SetFrac(raw, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(info.TokenAmount.Decimals)), nil)))
		}
	}
	return out, nil
}

// hyperliquidCash is the account's USDC including unrealized perp PnL, the
// quantity the replay of deposits, withdrawals, realized PnL, fees and
// funding must reach once open positions' unrealized PnL is added.
func hyperliquidCash(net *netcache.Client, u string) (*big.Rat, error) {
	post := func(kind string) ([]byte, error) {
		body, _ := json.Marshal(map[string]string{"type": kind, "user": u})
		return net.Fetch(netcache.Request{Key: "balance/hyperliquid/" + u + "/" + kind, URL: "https://api.hyperliquid.xyz/info", Body: body})
	}
	b, err := post("spotClearinghouseState")
	if err != nil {
		return nil, err
	}
	var spot struct {
		Balances []struct {
			Coin  string `json:"coin"`
			Total string `json:"total"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(b, &spot); err != nil {
		return nil, err
	}
	cash := new(big.Rat)
	for _, bal := range spot.Balances {
		v, err := decimal.Parse(bal.Total)
		if err != nil {
			return nil, err
		}
		if bal.Coin == "USDC" {
			cash.Add(cash, v)
		} else if v.Sign() != 0 {
			return nil, fmt.Errorf("hyperliquid spot balance of %s is not supported", bal.Coin)
		}
	}
	b, err = post("clearinghouseState")
	if err != nil {
		return nil, err
	}
	var perp struct {
		MarginSummary struct {
			AccountValue string `json:"accountValue"`
		} `json:"marginSummary"`
		AssetPositions []struct {
			Position struct {
				UnrealizedPnl string `json:"unrealizedPnl"`
			} `json:"position"`
		} `json:"assetPositions"`
	}
	if err := json.Unmarshal(b, &perp); err != nil {
		return nil, err
	}
	// Under unified margin the spot USDC balance already holds the perp
	// collateral; the replay knows only realized flows, so add back what
	// open positions have not yet realized.
	for _, p := range perp.AssetPositions {
		v, err := decimal.Parse(p.Position.UnrealizedPnl)
		if err != nil {
			return nil, err
		}
		cash.Sub(cash, v)
	}
	return cash, nil
}

func orZero(v *big.Rat) *big.Rat {
	if v == nil {
		return new(big.Rat)
	}
	return v
}

func sorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
