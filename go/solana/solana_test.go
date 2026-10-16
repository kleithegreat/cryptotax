package solana

import (
	"encoding/json"
	"testing"

	"github.com/kevin/cryptotax/decimal"
)

const (
	w     = "Wa11et11111111111111111111111111111111111111"
	usdc  = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	meme  = "Meme111111111111111111111111111111111111pump"
	other = "0ther111111111111111111111111111111111111111"
)

func tx(t *testing.T, js string) *rawTx {
	t.Helper()
	var r rawTx
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func amounts(t *testing.T, r *rawTx) map[string]string {
	t.Helper()
	m, ok, err := r.movement(w)
	if err != nil || !ok {
		t.Fatalf("movement: ok=%v err=%v", ok, err)
	}
	out := map[string]string{}
	for _, d := range m.Deltas {
		out[d.Asset.Key] = decimal.String(d.Amount)
	}
	if m.Fee != nil {
		out["fee"] = decimal.String(m.Fee.Amount)
	}
	return out
}

// A bonding-curve buy: the wallet pays the fee, funds rent for its own new
// token account (which must net out), and spends 0.1 SOL for 500 tokens.
func TestBuyNetsOwnRentAndSeparatesFee(t *testing.T) {
	r := tx(t, `{
	  "blockTime": 1754248955,
	  "meta": {"err": null, "fee": 5000,
	    "preBalances":  [1000000000, 0,       500000000, 2039280, 0],
	    "postBalances": [897955720,  2039280, 599000000, 2039280, 1000000],
	    "preTokenBalances":  [{"accountIndex": 3, "mint": "`+meme+`", "owner": "Curve", "uiTokenAmount": {"amount": "1000000000", "decimals": 6}}],
	    "postTokenBalances": [{"accountIndex": 3, "mint": "`+meme+`", "owner": "Curve", "uiTokenAmount": {"amount": "500000000", "decimals": 6}},
	                          {"accountIndex": 1, "mint": "`+meme+`", "owner": "`+w+`", "uiTokenAmount": {"amount": "500000000", "decimals": 6}}]},
	  "transaction": {"signatures": ["sig1"], "message": {
	    "accountKeys": [{"pubkey": "`+w+`", "signer": true}, {"pubkey": "MyAta"}, {"pubkey": "Curve"}, {"pubkey": "CurveAta"}, {"pubkey": "FeeVault"}],
	    "instructions": [{"programId": "Pump"}]}}}`)
	got := amounts(t, r)
	want := map[string]string{"SOL": "-0.1", "solana:" + meme: "500", "fee": "0.000005"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	m, _, _ := r.movement(w)
	if !m.Signed || len(m.Counterparties) != 2 || m.Counterparties[0] != "Curve" || m.Counterparties[1] != "FeeVault" {
		t.Errorf("signed=%v counterparties=%v", m.Signed, m.Counterparties)
	}
	if acc := r.ownedTokenAccounts(w); len(acc) != 1 || acc[0] != "MyAta" {
		t.Errorf("owned token accounts = %v", acc)
	}
}

func TestFailedTransactionCostsOnlyTheFee(t *testing.T) {
	r := tx(t, `{"blockTime": 1, "meta": {"err": {"InstructionError": [0, "Custom"]}, "fee": 5000,
	  "preBalances": [1000000000, 7], "postBalances": [999995000, 7], "preTokenBalances": [], "postTokenBalances": []},
	  "transaction": {"signatures": ["sig2"], "message": {"accountKeys": [{"pubkey": "`+w+`", "signer": true}, {"pubkey": "X"}], "instructions": []}}}`)
	got := amounts(t, r)
	if len(got) != 1 || got["fee"] != "0.000005" {
		t.Errorf("got %v", got)
	}
}

// An inbound USDC transfer to an existing token account never names the
// owner, is unsigned by it, and maps to the cash asset.
func TestUnsignedStablecoinInflow(t *testing.T) {
	r := tx(t, `{"blockTime": 1, "meta": {"err": null, "fee": 5000,
	  "preBalances": [10000000, 2039280, 2039280], "postBalances": [9995000, 2039280, 2039280],
	  "preTokenBalances":  [{"accountIndex": 1, "mint": "`+usdc+`", "owner": "`+other+`", "uiTokenAmount": {"amount": "10000000", "decimals": 6}},
	                        {"accountIndex": 2, "mint": "`+usdc+`", "owner": "`+w+`", "uiTokenAmount": {"amount": "0", "decimals": 6}}],
	  "postTokenBalances": [{"accountIndex": 1, "mint": "`+usdc+`", "owner": "`+other+`", "uiTokenAmount": {"amount": "5000000", "decimals": 6}},
	                        {"accountIndex": 2, "mint": "`+usdc+`", "owner": "`+w+`", "uiTokenAmount": {"amount": "5000000", "decimals": 6}}]},
	  "transaction": {"signatures": ["sig3"], "message": {"accountKeys": [{"pubkey": "`+other+`", "signer": true}, {"pubkey": "TheirAta"}, {"pubkey": "MyAta"}], "instructions": []}}}`)
	m, ok, err := r.movement(w)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if m.Signed || m.Fee != nil || len(m.Deltas) != 1 || m.Deltas[0].Asset.Key != "USD" || decimal.String(m.Deltas[0].Amount) != "5" {
		t.Errorf("movement %+v", m)
	}
	if len(m.Counterparties) != 1 || m.Counterparties[0] != other {
		t.Errorf("counterparties %v", m.Counterparties)
	}
}

// Wrapping SOL moves lamports into the wallet's own wSOL account: no change.
func TestWrapIsNotAMovement(t *testing.T) {
	r := tx(t, `{"blockTime": 1, "meta": {"err": null, "fee": 5000,
	  "preBalances": [2000000000, 0], "postBalances": [997955720, 1002039280],
	  "preTokenBalances": [],
	  "postTokenBalances": [{"accountIndex": 1, "mint": "So11111111111111111111111111111111111111112", "owner": "`+w+`", "uiTokenAmount": {"amount": "1000000000", "decimals": 9}}]},
	  "transaction": {"signatures": ["sig4"], "message": {"accountKeys": [{"pubkey": "`+w+`", "signer": true}, {"pubkey": "WsolAta"}], "instructions": []}}}`)
	got := amounts(t, r)
	if len(got) != 1 || got["fee"] != "0.000005" {
		t.Errorf("got %v", got)
	}
}
