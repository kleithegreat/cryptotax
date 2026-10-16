// Package solana derives exact per-transaction balance movements for a wallet
// from raw RPC transactions (pre/post balances), not from any indexer's
// interpretation. See docs/solana.md.
package solana

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/ledger"
	"github.com/kevin/cryptotax/netcache"
)

const (
	tokenProgram     = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	token2022Program = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
	computeBudget    = "ComputeBudget111111111111111111111111111111"
	wsolMint         = "So11111111111111111111111111111111111111112"
	sigPage          = 1000
)

type Fetcher struct {
	Net    *netcache.Client
	APIKey string
}

func (f *Fetcher) rpcURL() string {
	return "https://mainnet.helius-rpc.com/?api-key=" + f.APIKey
}

func (f *Fetcher) call(key string, immutable bool, method string, params ...any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	b, err := f.Net.Fetch(netcache.Request{Key: key, URL: f.rpcURL(), Body: body, Immutable: immutable})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: RPC error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// Movements returns one movement per transaction that changed the wallet.
func (f *Fetcher) Movements(wallet string) ([]ledger.Movement, error) {
	pending := []string{wallet}
	for _, program := range []string{tokenProgram, token2022Program} {
		accts, err := f.tokenAccounts(wallet, program)
		if err != nil {
			return nil, err
		}
		pending = append(pending, accts...)
	}

	// Token accounts that were closed are not listed by the RPC, and an
	// inbound transfer to one never mentions the owner. Every owned token
	// account seen in any fetched transaction is therefore crawled too,
	// until no new account appears.
	crawled := map[string]bool{}
	txs := map[string]*rawTx{}
	for len(pending) > 0 {
		addr := pending[0]
		pending = pending[1:]
		if crawled[addr] {
			continue
		}
		crawled[addr] = true
		sigs, err := f.signatures(addr)
		if err != nil {
			return nil, err
		}
		for _, sig := range sigs {
			if txs[sig] != nil {
				continue
			}
			tx, err := f.transaction(sig)
			if err != nil {
				return nil, err
			}
			txs[sig] = tx
			for _, a := range tx.ownedTokenAccounts(wallet) {
				if !crawled[a] {
					pending = append(pending, a)
				}
			}
		}
	}

	var out []ledger.Movement
	for sig, tx := range txs {
		m, ok, err := tx.movement(wallet)
		if err != nil {
			return nil, fmt.Errorf("solana tx %s: %w", sig, err)
		}
		if ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *Fetcher) tokenAccounts(wallet, program string) ([]string, error) {
	res, err := f.call("solana/token-accounts/"+wallet+"/"+program, false, "getTokenAccountsByOwner",
		wallet, map[string]string{"programId": program}, map[string]string{"encoding": "jsonParsed"})
	if err != nil {
		return nil, err
	}
	var r struct {
		Value []struct {
			Pubkey string `json:"pubkey"`
		} `json:"value"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range r.Value {
		out = append(out, v.Pubkey)
	}
	return out, nil
}

// signatures lists every signature that touched addr, failed ones included
// (a failed transaction still costs its fee payer the fee).
func (f *Fetcher) signatures(addr string) ([]string, error) {
	var out []string
	before := ""
	for {
		opts := map[string]any{"limit": sigPage}
		key := "solana/sigs/" + addr
		if before != "" {
			opts["before"] = before
			key += "/before-" + before
		}
		// Pages anchored at an existing signature never change; only the
		// newest page can grow.
		res, err := f.call(key, before != "", "getSignaturesForAddress", addr, opts)
		if err != nil {
			return nil, err
		}
		var page []struct {
			Signature string `json:"signature"`
		}
		if err := json.Unmarshal(res, &page); err != nil {
			return nil, err
		}
		for _, p := range page {
			out = append(out, p.Signature)
		}
		if len(page) < sigPage {
			return out, nil
		}
		before = page[len(page)-1].Signature
	}
}

func (f *Fetcher) transaction(sig string) (*rawTx, error) {
	res, err := f.call("solana/tx/"+sig, true, "getTransaction", sig,
		map[string]any{"encoding": "jsonParsed", "maxSupportedTransactionVersion": 0, "commitment": "finalized"})
	if err != nil {
		return nil, err
	}
	if string(res) == "null" {
		return nil, fmt.Errorf("transaction %s not found", sig)
	}
	var tx rawTx
	if err := json.Unmarshal(res, &tx); err != nil {
		return nil, fmt.Errorf("transaction %s: %w", sig, err)
	}
	return &tx, nil
}

type tokenBalance struct {
	AccountIndex  int    `json:"accountIndex"`
	Mint          string `json:"mint"`
	Owner         string `json:"owner"`
	UITokenAmount struct {
		Amount   string `json:"amount"`
		Decimals int    `json:"decimals"`
	} `json:"uiTokenAmount"`
}

type rawTx struct {
	BlockTime *int64 `json:"blockTime"`
	Meta      struct {
		Err               json.RawMessage `json:"err"`
		Fee               int64           `json:"fee"`
		PreBalances       []int64         `json:"preBalances"`
		PostBalances      []int64         `json:"postBalances"`
		PreTokenBalances  []tokenBalance  `json:"preTokenBalances"`
		PostTokenBalances []tokenBalance  `json:"postTokenBalances"`
	} `json:"meta"`
	Transaction struct {
		Signatures []string `json:"signatures"`
		Message    struct {
			AccountKeys []struct {
				Pubkey string `json:"pubkey"`
				Signer bool   `json:"signer"`
			} `json:"accountKeys"`
			Instructions []struct {
				ProgramID string `json:"programId"`
			} `json:"instructions"`
		} `json:"message"`
	} `json:"transaction"`
}

func (tx *rawTx) ownedTokenAccounts(wallet string) []string {
	var out []string
	for _, tb := range append(append([]tokenBalance{}, tx.Meta.PreTokenBalances...), tx.Meta.PostTokenBalances...) {
		if tb.Owner == wallet && tb.AccountIndex < len(tx.Transaction.Message.AccountKeys) {
			out = append(out, tx.Transaction.Message.AccountKeys[tb.AccountIndex].Pubkey)
		}
	}
	return out
}

var lamportsPerSOL = big.NewInt(1_000_000_000)

// movement computes the wallet's net change. Lamports held by the wallet's
// own token accounts count as the wallet's SOL, so rent deposits into its own
// accounts and wrapping SOL net to zero; wSOL token amounts are therefore
// excluded (their lamports are already counted).
func (tx *rawTx) movement(wallet string) (ledger.Movement, bool, error) {
	keys := tx.Transaction.Message.AccountKeys
	if tx.BlockTime == nil {
		return ledger.Movement{}, false, fmt.Errorf("no block time")
	}
	if len(tx.Meta.PreBalances) != len(keys) || len(tx.Meta.PostBalances) != len(keys) {
		return ledger.Movement{}, false, fmt.Errorf("balance arrays do not match account keys")
	}

	owned := map[int]bool{}
	tokenOwner := map[int]string{}
	for _, tb := range append(append([]tokenBalance{}, tx.Meta.PreTokenBalances...), tx.Meta.PostTokenBalances...) {
		tokenOwner[tb.AccountIndex] = tb.Owner
		if tb.Owner == wallet {
			owned[tb.AccountIndex] = true
		}
	}

	signed := false
	var lamports int64
	for i, k := range keys {
		if k.Pubkey == wallet {
			owned[i] = true
			signed = signed || k.Signer
		}
	}
	for i := range owned {
		lamports += tx.Meta.PostBalances[i] - tx.Meta.PreBalances[i]
	}

	feePayer := len(keys) > 0 && keys[0].Pubkey == wallet
	m := ledger.Movement{
		ID:      "solana:" + tx.Transaction.Signatures[0],
		Time:    time.Unix(*tx.BlockTime, 0).UTC(),
		Account: "solana:" + wallet,
		Signed:  signed,
		Note:    tx.programs(),
	}
	if feePayer && tx.Meta.Fee > 0 {
		lamports += tx.Meta.Fee
		m.Fee = &ledger.Delta{Asset: asset.Native("solana"), Amount: new(big.Rat).SetFrac(big.NewInt(tx.Meta.Fee), lamportsPerSOL)}
	}
	if lamports != 0 {
		m.Deltas = append(m.Deltas, ledger.Delta{Asset: asset.Native("solana"), Amount: new(big.Rat).SetFrac(big.NewInt(lamports), lamportsPerSOL)})
	}

	// Token deltas per mint, and counterparty owners per mint.
	type acc struct {
		raw      *big.Int
		decimals int
	}
	byMint := map[string]*acc{}
	others := map[string]map[string]*big.Int{} // mint -> owner -> raw delta
	add := func(tb tokenBalance, sign int64) error {
		raw, ok := new(big.Int).SetString(tb.UITokenAmount.Amount, 10)
		if !ok {
			return fmt.Errorf("token amount %q", tb.UITokenAmount.Amount)
		}
		raw.Mul(raw, big.NewInt(sign))
		if tb.Owner != wallet {
			if others[tb.Mint] == nil {
				others[tb.Mint] = map[string]*big.Int{}
			}
			if others[tb.Mint][tb.Owner] == nil {
				others[tb.Mint][tb.Owner] = new(big.Int)
			}
			others[tb.Mint][tb.Owner].Add(others[tb.Mint][tb.Owner], raw)
			return nil
		}
		if tb.Mint == wsolMint {
			return nil
		}
		a := byMint[tb.Mint]
		if a == nil {
			a = &acc{raw: new(big.Int), decimals: tb.UITokenAmount.Decimals}
			byMint[tb.Mint] = a
		}
		a.raw.Add(a.raw, raw)
		return nil
	}
	for _, tb := range tx.Meta.PreTokenBalances {
		if err := add(tb, -1); err != nil {
			return m, false, err
		}
	}
	for _, tb := range tx.Meta.PostTokenBalances {
		if err := add(tb, 1); err != nil {
			return m, false, err
		}
	}
	var mints []string
	for mint, a := range byMint {
		if a.raw.Sign() != 0 {
			mints = append(mints, mint)
		}
	}
	sort.Strings(mints)
	for _, mint := range mints {
		a := byMint[mint]
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(a.decimals)), nil)
		m.Deltas = append(m.Deltas, ledger.Delta{Asset: asset.Token("solana", mint, ""), Amount: new(big.Rat).SetFrac(a.raw, scale)})
	}

	// Counterparties: external parties whose balance moved opposite to ours.
	cps := map[string]bool{}
	if lamports != 0 {
		for i, k := range keys {
			if owned[i] || tokenOwner[i] != "" {
				continue
			}
			if delta := tx.Meta.PostBalances[i] - tx.Meta.PreBalances[i]; delta != 0 && (delta > 0) == (lamports < 0) {
				cps[k.Pubkey] = true
			}
		}
	}
	for _, mint := range mints {
		for owner, raw := range others[mint] {
			if raw.Sign() != 0 && raw.Sign() == -byMint[mint].raw.Sign() {
				cps[owner] = true
			}
		}
	}
	for c := range cps {
		m.Counterparties = append(m.Counterparties, c)
	}
	sort.Strings(m.Counterparties)

	return m, len(m.Deltas) > 0 || m.Fee != nil, nil
}

func (tx *rawTx) programs() string {
	seen := map[string]bool{}
	var out []string
	for _, in := range tx.Transaction.Message.Instructions {
		if in.ProgramID == computeBudget || seen[in.ProgramID] {
			continue
		}
		seen[in.ProgramID] = true
		out = append(out, in.ProgramID)
	}
	note := strings.Join(out, ",")
	if len(tx.Meta.Err) > 0 && string(tx.Meta.Err) != "null" {
		note = "FAILED " + note
	}
	return note
}
